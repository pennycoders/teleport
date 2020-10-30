/*
Copyright 2019 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package services

import (
	"context"
	"fmt"
	"time"

	"github.com/gravitational/teleport/lib/utils"
	"github.com/gravitational/teleport/lib/utils/parse"

	"github.com/gravitational/trace"

	"github.com/jonboulle/clockwork"
	"github.com/pborman/uuid"
)

const (
	RequestStrategyOptional = "optional"
	RequestStrategyReason   = "reason"
	RequestStrategyAlways   = "always"
)

// RequestIDs is a collection of IDs for privilege escalation requests.
type RequestIDs struct {
	AccessRequests []string `json:"access_requests,omitempty"`
}

func (r *RequestIDs) Marshal() ([]byte, error) {
	data, err := utils.FastMarshal(r)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	return data, nil
}

func (r *RequestIDs) Unmarshal(data []byte) error {
	if err := utils.FastUnmarshal(data, r); err != nil {
		return trace.Wrap(err)
	}
	return trace.Wrap(r.Check())
}

func (r *RequestIDs) Check() error {
	for _, id := range r.AccessRequests {
		if uuid.Parse(id) == nil {
			return trace.BadParameter("invalid request id %q", id)
		}
	}
	return nil
}

func (r *RequestIDs) IsEmpty() bool {
	return len(r.AccessRequests) < 1
}

// stateVariants allows iteration of the expected variants
// of RequestState.
var stateVariants = [4]RequestState{
	RequestState_NONE,
	RequestState_PENDING,
	RequestState_APPROVED,
	RequestState_DENIED,
}

// Parse attempts to interpret a value as a string representation
// of a RequestState.
func (s *RequestState) Parse(val string) error {
	for _, state := range stateVariants {
		if state.String() == val {
			*s = state
			return nil
		}
	}
	return trace.BadParameter("unknown request state: %q", val)
}

// key values for map encoding of request filter
const (
	keyID    = "id"
	keyUser  = "user"
	keyState = "state"
)

func (f *AccessRequestFilter) IntoMap() map[string]string {
	m := make(map[string]string)
	if f.ID != "" {
		m[keyID] = f.ID
	}
	if f.User != "" {
		m[keyUser] = f.User
	}
	if !f.State.IsNone() {
		m[keyState] = f.State.String()
	}
	return m
}

func (f *AccessRequestFilter) FromMap(m map[string]string) error {
	for key, val := range m {
		switch key {
		case keyID:
			f.ID = val
		case keyUser:
			f.User = val
		case keyState:
			if err := f.State.Parse(val); err != nil {
				return trace.Wrap(err)
			}
		default:
			return trace.BadParameter("unknown filter key %s", key)
		}
	}
	return nil
}

// Match checks if a given access request matches this filter.
func (f *AccessRequestFilter) Match(req AccessRequest) bool {
	if f.ID != "" && req.GetName() != f.ID {
		return false
	}
	if f.User != "" && req.GetUser() != f.User {
		return false
	}
	if !f.State.IsNone() && req.GetState() != f.State {
		return false
	}
	return true
}

func (f *AccessRequestFilter) Equals(o AccessRequestFilter) bool {
	return f.ID == o.ID && f.User == o.User && f.State == o.State
}

type AccessRequestUpdate struct {
	RequestID   string
	State       RequestState
	Reason      string
	Annotations map[string][]string
	Roles       []string
}

func (u *AccessRequestUpdate) Check() error {
	if u.RequestID == "" {
		return trace.BadParameter("missing request id")
	}
	if u.State.IsNone() {
		return trace.BadParameter("missing request state")
	}
	if len(u.Roles) > 0 && !u.State.IsApproved() {
		return trace.BadParameter("cannot override roles when setting state: %s", u.State)
	}
	return nil
}

// DynamicAccess is a service which manages dynamic RBAC.
type DynamicAccess interface {
	// CreateAccessRequest stores a new access request.
	CreateAccessRequest(ctx context.Context, req AccessRequest) error
	// SetAccessRequestState updates the state of an existing access request.
	SetAccessRequestState(ctx context.Context, params AccessRequestUpdate) error
	// GetAccessRequests gets all currently active access requests.
	GetAccessRequests(ctx context.Context, filter AccessRequestFilter) ([]AccessRequest, error)
	// DeleteAccessRequest deletes an access request.
	DeleteAccessRequest(ctx context.Context, reqID string) error
	// GetPluginData loads all plugin data matching the supplied filter.
	GetPluginData(ctx context.Context, filter PluginDataFilter) ([]PluginData, error)
	// UpdatePluginData updates a per-resource PluginData entry.
	UpdatePluginData(ctx context.Context, params PluginDataUpdateParams) error
}

// DynamicAccessExt is an extended dynamic access interface
// used to implement some auth server internals.
type DynamicAccessExt interface {
	DynamicAccess
	// UpsertAccessRequest creates or updates an access request.
	UpsertAccessRequest(ctx context.Context, req AccessRequest) error
	// DeleteAllAccessRequests deletes all existent access requests.
	DeleteAllAccessRequests(ctx context.Context) error
}

// AccessRequest is a request for temporarily granted roles
type AccessRequest interface {
	Resource
	// GetUser gets the name of the requesting user
	GetUser() string
	// GetRoles gets the roles being requested by the user
	GetRoles() []string
	// SetRoles overrides the roles being requested by the user
	SetRoles([]string)
	// GetState gets the current state of the request
	GetState() RequestState
	// SetState sets the approval state of the request
	SetState(RequestState) error
	// GetCreationTime gets the time at which the request was
	// originally registered with the auth server.
	GetCreationTime() time.Time
	// SetCreationTime sets the creation time of the request.
	SetCreationTime(time.Time)
	// GetAccessExpiry gets the upper limit for which this request
	// may be considered active.
	GetAccessExpiry() time.Time
	// SetAccessExpiry sets the upper limit for which this request
	// may be considered active.
	SetAccessExpiry(time.Time)

	GetRequestReason() string
	SetRequestReason(string)
	GetResolveReason() string
	SetResolveReason(string)
	GetResolveAnnotations() map[string][]string
	SetResolveAnnotations(map[string][]string)
	GetSystemAnnotations() map[string][]string
	SetSystemAnnotations(map[string][]string)

	// CheckAndSetDefaults validates the access request and
	// supplies default values where appropriate.
	CheckAndSetDefaults() error
	// Equals checks equality between access request values.
	Equals(AccessRequest) bool
}

// GetAccessRequest is a helper function assists with loading a specific request by ID.
func GetAccessRequest(ctx context.Context, acc DynamicAccess, reqID string) (AccessRequest, error) {
	reqs, err := acc.GetAccessRequests(ctx, AccessRequestFilter{
		ID: reqID,
	})
	if err != nil {
		return nil, trace.Wrap(err)
	}
	if len(reqs) < 1 {
		return nil, trace.NotFound("no access request matching %q", reqID)
	}
	return reqs[0], nil
}

func (s RequestState) IsNone() bool {
	return s == RequestState_NONE
}

func (s RequestState) IsPending() bool {
	return s == RequestState_PENDING
}

func (s RequestState) IsApproved() bool {
	return s == RequestState_APPROVED
}

func (s RequestState) IsDenied() bool {
	return s == RequestState_DENIED
}

// NewAccessRequest assembled an AccessReqeust resource.
func NewAccessRequest(user string, roles ...string) (AccessRequest, error) {
	req := AccessRequestV3{
		Kind:    KindAccessRequest,
		Version: V3,
		Metadata: Metadata{
			Name: uuid.New(),
		},
		Spec: AccessRequestSpecV3{
			User:  user,
			Roles: roles,
			State: RequestState_PENDING,
		},
	}
	if err := req.CheckAndSetDefaults(); err != nil {
		return nil, trace.Wrap(err)
	}
	return &req, nil
}

func (c AccessRequestConditions) GetTraitMappings() TraitMappingSet {
	tm := make([]TraitMapping, 0, len(c.ClaimsToRoles))
	for _, mapping := range c.ClaimsToRoles {
		tm = append(tm, TraitMapping{
			Trait: mapping.Claim,
			Value: mapping.Value,
			Roles: mapping.Roles,
		})
	}
	return TraitMappingSet(tm)
}

type UserAndRoleGetter interface {
	UserGetter
	RoleGetter
	GetRoles() ([]Role, error)
}

type requestValidator struct {
	traits map[string][]string
	state  RequestState
	Roles  struct {
		Allow, Deny []parse.Matcher
	}
	Annotations struct {
		Allow, Deny map[string][]string
	}
}

func newRequestValidator(traits map[string][]string, state RequestState) requestValidator {
	m := requestValidator{
		traits: traits,
		state:  state,
	}
	if m.state.IsPending() {
		m.Annotations.Allow = make(map[string][]string)
		m.Annotations.Deny = make(map[string][]string)
	}
	return m
}

func (m *requestValidator) push(role Role) error {
	for _, d := range role.GetAccessRequestConditions(Deny).Roles {
		md, err := parse.NewMatcher(d)
		if err != nil {
			return trace.Wrap(err)
		}
		m.Roles.Deny = append(m.Roles.Deny, md)
	}

	for _, d := range role.GetAccessRequestConditions(Deny).GetTraitMappings().TraitsToRoles(m.traits) {
		md, err := parse.NewMatcher(d)
		if err != nil {
			return trace.Wrap(err)
		}
		m.Roles.Deny = append(m.Roles.Deny, md)
	}

	if m.state.IsPending() {
		for kd, vd := range role.GetAccessRequestConditions(Deny).Annotations {
			var vals []string
		Dval:
			for _, d := range vd {
				applied, err := applyValueTraits(d, m.traits)
				if err != nil {
					continue Dval
				}
				vals = append(vals, applied...)
			}
			m.Annotations.Deny[kd] = append(m.Annotations.Deny[kd], vals...)
		}
	}

	for _, a := range role.GetAccessRequestConditions(Allow).Roles {
		ma, err := parse.NewMatcher(a)
		if err != nil {
			return trace.Wrap(err)
		}
		m.Roles.Allow = append(m.Roles.Allow, ma)
	}

	for _, a := range role.GetAccessRequestConditions(Allow).GetTraitMappings().TraitsToRoles(m.traits) {
		ma, err := parse.NewMatcher(a)
		if err != nil {
			return trace.Wrap(err)
		}
		m.Roles.Allow = append(m.Roles.Allow, ma)
	}

	if m.state.IsPending() {
		for ka, va := range role.GetAccessRequestConditions(Allow).Annotations {
			var vals []string
		Avals:
			for _, a := range va {
				applied, err := applyValueTraits(a, m.traits)
				if err != nil {
					continue Avals
				}
				vals = append(vals, applied...)
			}
			m.Annotations.Allow[ka] = append(m.Annotations.Allow[ka], vals...)
		}
	}
	return nil
}

func (m *requestValidator) CanRequestRole(name string) bool {
	for _, deny := range m.Roles.Deny {
		if deny.Match(name) {
			return false
		}
	}
	for _, allow := range m.Roles.Allow {
		if allow.Match(name) {
			return true
		}
	}
	return false
}

func (m *requestValidator) SystemAnnotations() map[string][]string {
	annotations := make(map[string][]string)
	for k, va := range m.Annotations.Allow {
		var filtered []string
		for _, v := range va {
			if !utils.SliceContainsStr(m.Annotations.Deny[k], v) {
				filtered = append(filtered, v)
			}
		}
		if len(filtered) == 0 {
			continue
		}
		annotations[k] = filtered
	}
	return annotations
}

func ValidateAccessRequest(getter UserAndRoleGetter, req AccessRequest, expandRoles bool) error {
	user, err := getter.GetUser(req.GetUser(), false)
	if err != nil {
		return trace.Wrap(err)
	}

	var requireReason bool
	validator := newRequestValidator(user.GetTraits(), req.GetState())

	for _, roleName := range user.GetRoles() {
		role, err := getter.GetRole(roleName)
		if err != nil {
			return trace.Wrap(err)
		}
		if err := validator.push(role); err != nil {
			return trace.Wrap(err)
		}
		if role.GetOptions().RequestAccess == RequestStrategyReason {
			requireReason = true
		}
	}

	if requireReason && req.GetRequestReason() == "" {
		return trace.BadParameter("request reason must be specified")
	}

	if r := req.GetRoles(); len(r) == 1 && r[0] == "*" {
		if !expandRoles {
			return trace.BadParameter("unexpected wildcard access request")
		}
		allRoles, err := getter.GetRoles()
		if err != nil {
			return trace.Wrap(err)
		}

		held := make(map[string]bool, len(user.GetRoles()))
		for _, roleName := range user.GetRoles() {
			held[roleName] = true
		}
		var expanded []string
		for _, role := range allRoles {
			if n := role.GetName(); !held[n] && validator.CanRequestRole(n) {
				expanded = append(expanded, n)
			}
		}
		req.SetRoles(expanded)
	}

	for _, roleName := range req.GetRoles() {
		if !validator.CanRequestRole(roleName) {
			return trace.BadParameter("user %q cannot request/assume role %q", req.GetUser(), roleName)
		}
	}

	if req.GetState().IsPending() {
		req.SetSystemAnnotations(validator.SystemAnnotations())
	}

	return nil
}

func (r *AccessRequestV3) GetUser() string {
	return r.Spec.User
}

func (r *AccessRequestV3) GetRoles() []string {
	return r.Spec.Roles
}

func (r *AccessRequestV3) SetRoles(roles []string) {
	r.Spec.Roles = roles
}

func (r *AccessRequestV3) GetState() RequestState {
	return r.Spec.State
}

func (r *AccessRequestV3) SetState(state RequestState) error {
	if r.Spec.State.IsDenied() {
		if state.IsDenied() {
			return nil
		}
		return trace.BadParameter("cannot set request-state %q (already denied)", state.String())
	}
	r.Spec.State = state
	return nil
}

func (r *AccessRequestV3) GetCreationTime() time.Time {
	return r.Spec.Created
}

func (r *AccessRequestV3) SetCreationTime(t time.Time) {
	r.Spec.Created = t
}

func (r *AccessRequestV3) GetAccessExpiry() time.Time {
	return r.Spec.Expires
}

func (r *AccessRequestV3) SetAccessExpiry(expiry time.Time) {
	r.Spec.Expires = expiry
}

func (r *AccessRequestV3) GetRequestReason() string {
	return r.Spec.RequestReason
}

func (r *AccessRequestV3) SetRequestReason(reason string) {
	r.Spec.RequestReason = reason
}

func (r *AccessRequestV3) GetResolveReason() string {
	return r.Spec.ResolveReason
}

func (r *AccessRequestV3) SetResolveReason(reason string) {
	r.Spec.ResolveReason = reason
}

func (r *AccessRequestV3) GetResolveAnnotations() map[string][]string {
	return r.Spec.ResolveAnnotations
}

func (r *AccessRequestV3) SetResolveAnnotations(annotations map[string][]string) {
	r.Spec.ResolveAnnotations = annotations
}

func (r *AccessRequestV3) GetSystemAnnotations() map[string][]string {
	return r.Spec.SystemAnnotations
}

func (r *AccessRequestV3) SetSystemAnnotations(annotations map[string][]string) {
	r.Spec.SystemAnnotations = annotations
}

func (r *AccessRequestV3) CheckAndSetDefaults() error {
	if err := r.Metadata.CheckAndSetDefaults(); err != nil {
		return trace.Wrap(err)
	}
	if r.GetState().IsNone() {
		if err := r.SetState(RequestState_PENDING); err != nil {
			return trace.Wrap(err)
		}
	}
	if err := r.Check(); err != nil {
		return trace.Wrap(err)
	}
	return nil
}

func (r *AccessRequestV3) Check() error {
	if r.Kind == "" {
		return trace.BadParameter("access request kind not set")
	}
	if r.Version == "" {
		return trace.BadParameter("access request version not set")
	}
	if r.GetName() == "" {
		return trace.BadParameter("access request id not set")
	}
	if uuid.Parse(r.GetName()) == nil {
		return trace.BadParameter("invalid access request id %q", r.GetName())
	}
	if r.GetUser() == "" {
		return trace.BadParameter("access request user name not set")
	}
	if len(r.GetRoles()) < 1 {
		return trace.BadParameter("access request does not specify any roles")
	}
	if r.GetState().IsPending() {
		if r.GetResolveReason() != "" {
			return trace.BadParameter("pending requests cannot include resolve reason")
		}
		if len(r.GetResolveAnnotations()) != 0 {
			return trace.BadParameter("pending requests cannot include resolve annotations")
		}
	}
	return nil
}

func (r *AccessRequestV3) Equals(other AccessRequest) bool {
	o, ok := other.(*AccessRequestV3)
	if !ok {
		return false
	}
	if r.GetName() != o.GetName() {
		return false
	}
	return r.Spec.Equals(&o.Spec)
}

func (s *AccessRequestSpecV3) Equals(other *AccessRequestSpecV3) bool {
	if s.User != other.User {
		return false
	}
	if len(s.Roles) != len(other.Roles) {
		return false
	}
	for i, role := range s.Roles {
		if role != other.Roles[i] {
			return false
		}
	}
	if s.Created != other.Created {
		return false
	}
	if s.Expires != other.Expires {
		return false
	}
	return s.State == other.State
}

type AccessRequestMarshaler interface {
	MarshalAccessRequest(req AccessRequest, opts ...MarshalOption) ([]byte, error)
	UnmarshalAccessRequest(bytes []byte, opts ...MarshalOption) (AccessRequest, error)
}

type accessRequestMarshaler struct{}

func (r *accessRequestMarshaler) MarshalAccessRequest(req AccessRequest, opts ...MarshalOption) ([]byte, error) {
	cfg, err := collectOptions(opts)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	switch r := req.(type) {
	case *AccessRequestV3:
		if !cfg.PreserveResourceID {
			// avoid modifying the original object
			// to prevent unexpected data races
			cp := *r
			cp.SetResourceID(0)
			r = &cp
		}
		return utils.FastMarshal(r)
	default:
		return nil, trace.BadParameter("unrecognized access request type: %T", req)
	}
}

func (r *accessRequestMarshaler) UnmarshalAccessRequest(data []byte, opts ...MarshalOption) (AccessRequest, error) {
	cfg, err := collectOptions(opts)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	var req AccessRequestV3
	if cfg.SkipValidation {
		if err := utils.FastUnmarshal(data, &req); err != nil {
			return nil, trace.Wrap(err)
		}
	} else {
		if err := utils.UnmarshalWithSchema(GetAccessRequestSchema(), &req, data); err != nil {
			return nil, trace.Wrap(err)
		}
	}
	if err := req.CheckAndSetDefaults(); err != nil {
		return nil, trace.Wrap(err)
	}
	if cfg.ID != 0 {
		req.SetResourceID(cfg.ID)
	}
	if !cfg.Expires.IsZero() {
		req.SetExpiry(cfg.Expires)
	}
	return &req, nil
}

var accessRequestMarshalerInstance AccessRequestMarshaler = &accessRequestMarshaler{}

func GetAccessRequestMarshaler() AccessRequestMarshaler {
	marshalerMutex.Lock()
	defer marshalerMutex.Unlock()
	return accessRequestMarshalerInstance
}

const AccessRequestSpecSchema = `{
	"type": "object",
	"additionalProperties": false,
	"properties": {
		"user": { "type": "string" },
		"roles": {
			"type": "array",
			"items": { "type": "string" }
		},
		"state": { "type": "integer" },
		"created": { "type": "string" },
		"expires": { "type": "string" },
		"request_reason": { "type": "string" },
		"resolve_reason": { "type": "string" },
		"resolve_annotations": { "type": "object" },
		"system_annotations": { "type": "object" }
	}
}`

func GetAccessRequestSchema() string {
	return fmt.Sprintf(V2SchemaTemplate, MetadataSchema, AccessRequestSpecSchema, DefaultDefinitions)
}

func (r *AccessRequestV3) GetKind() string {
	return r.Kind
}

func (r *AccessRequestV3) GetSubKind() string {
	return r.SubKind
}

func (r *AccessRequestV3) SetSubKind(subKind string) {
	r.SubKind = subKind
}

func (r *AccessRequestV3) GetVersion() string {
	return r.Version
}

func (r *AccessRequestV3) GetName() string {
	return r.Metadata.Name
}

func (r *AccessRequestV3) SetName(name string) {
	r.Metadata.Name = name
}

func (r *AccessRequestV3) Expiry() time.Time {
	return r.Metadata.Expiry()
}

func (r *AccessRequestV3) SetExpiry(expiry time.Time) {
	r.Metadata.SetExpiry(expiry)
}

func (r *AccessRequestV3) SetTTL(clock clockwork.Clock, ttl time.Duration) {
	r.Metadata.SetTTL(clock, ttl)
}

func (r *AccessRequestV3) GetMetadata() Metadata {
	return r.Metadata
}

func (r *AccessRequestV3) GetResourceID() int64 {
	return r.Metadata.GetID()
}

func (r *AccessRequestV3) SetResourceID(id int64) {
	r.Metadata.SetID(id)
}

func (r *AccessRequestV3) String() string {
	return fmt.Sprintf("AccessRequest(user=%v,roles=%+v)", r.Spec.User, r.Spec.Roles)
}
