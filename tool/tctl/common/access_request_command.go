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

package common

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gravitational/kingpin"
	"github.com/gravitational/teleport"
	"github.com/gravitational/teleport/lib/asciitable"
	"github.com/gravitational/teleport/lib/auth"
	"github.com/gravitational/teleport/lib/service"
	"github.com/gravitational/teleport/lib/services"
	"github.com/gravitational/trace"
)

// AccessRequestCommand implements `tctl users` set of commands
// It implements CLICommand interface
type AccessRequestCommand struct {
	config *service.Config
	reqIDs string

	user      string
	roles     string
	delegator string
	reason    string
	attrs     string
	// format is the output format, e.g. text or json
	format string

	requestList    *kingpin.CmdClause
	requestApprove *kingpin.CmdClause
	requestDeny    *kingpin.CmdClause
	requestCreate  *kingpin.CmdClause
	requestDelete  *kingpin.CmdClause
}

// Initialize allows AccessRequestCommand to plug itself into the CLI parser
func (c *AccessRequestCommand) Initialize(app *kingpin.Application, config *service.Config) {
	c.config = config
	requests := app.Command("requests", "Manage access requests").Alias("request")

	c.requestList = requests.Command("ls", "Show active access requests")
	c.requestList.Flag("format", "Output format, 'text' or 'json'").Hidden().Default(teleport.Text).StringVar(&c.format)

	c.requestApprove = requests.Command("approve", "Approve pending access request")
	c.requestApprove.Arg("request-id", "ID of target request(s)").Required().StringVar(&c.reqIDs)
	c.requestApprove.Flag("delegator", "Optional delegating identity").StringVar(&c.delegator)
	c.requestApprove.Flag("reason", "Optional reason message").StringVar(&c.reason)
	c.requestApprove.Flag("attrs", "Resolution attributes <key>=<val>[,...]").StringVar(&c.attrs)

	c.requestDeny = requests.Command("deny", "Deny pending access request")
	c.requestDeny.Arg("request-id", "ID of target request(s)").Required().StringVar(&c.reqIDs)
	c.requestDeny.Flag("delegator", "Optional delegating identity").StringVar(&c.delegator)
	c.requestDeny.Flag("reason", "Optional reason message").StringVar(&c.reason)
	c.requestDeny.Flag("attrs", "Resolution attributes <key>=<val>[,...]").StringVar(&c.attrs)

	c.requestCreate = requests.Command("create", "Create pending access request")
	c.requestCreate.Arg("username", "Name of target user").Required().StringVar(&c.user)
	c.requestCreate.Flag("roles", "Roles to be requested").Required().StringVar(&c.roles)

	c.requestDelete = requests.Command("rm", "Delete an access request")
	c.requestDelete.Arg("request-id", "ID of target request(s)").Required().StringVar(&c.reqIDs)
}

// TryRun takes the CLI command as an argument (like "access-request list") and executes it.
func (c *AccessRequestCommand) TryRun(cmd string, client auth.ClientI) (match bool, err error) {
	switch cmd {
	case c.requestList.FullCommand():
		err = c.List(client)
	case c.requestApprove.FullCommand():
		err = c.Approve(client)
	case c.requestDeny.FullCommand():
		err = c.Deny(client)
	case c.requestCreate.FullCommand():
		err = c.Create(client)
	case c.requestDelete.FullCommand():
		err = c.Delete(client)
	default:
		return false, nil
	}
	return true, trace.Wrap(err)
}

func (c *AccessRequestCommand) List(client auth.ClientI) error {
	reqs, err := client.GetAccessRequests(context.TODO(), services.AccessRequestFilter{})
	if err != nil {
		return trace.Wrap(err)
	}
	if err := c.PrintAccessRequests(client, reqs, c.format); err != nil {
		return trace.Wrap(err)
	}
	return nil
}

func (c *AccessRequestCommand) splitAttrs() (map[string]string, error) {
	attrs := make(map[string]string)
	for _, s := range strings.Split(c.attrs, ",") {
		if s == "" {
			continue
		}
		idx := strings.Index(s, "=")
		if idx < 1 {
			return nil, trace.BadParameter("invalid key-value pair: %q", s)
		}
		key, val := strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx+1:])
		if key == "" {
			return nil, trace.BadParameter("empty attr key")
		}
		if val == "" {
			return nil, trace.BadParameter("empty sttr val")
		}
		attrs[key] = val
	}
	return attrs, nil
}

func (c *AccessRequestCommand) Approve(client auth.ClientI) error {
	ctx := context.TODO()
	if c.delegator != "" {
		ctx = auth.WithDelegator(ctx, c.delegator)
	}
	attrs, err := c.splitAttrs()
	if err != nil {
		return trace.Wrap(err)
	}
	for _, reqID := range strings.Split(c.reqIDs, ",") {
		if err := client.SetAccessRequestState(ctx, services.AccessRequestUpdate{
			RequestID: reqID,
			State:     services.RequestState_APPROVED,
			Reason:    c.reason,
			Attrs:     attrs,
		}); err != nil {
			return trace.Wrap(err)
		}
	}
	return nil
}

func (c *AccessRequestCommand) Deny(client auth.ClientI) error {
	ctx := context.TODO()
	if c.delegator != "" {
		ctx = auth.WithDelegator(ctx, c.delegator)
	}
	attrs, err := c.splitAttrs()
	if err != nil {
		return trace.Wrap(err)
	}
	for _, reqID := range strings.Split(c.reqIDs, ",") {
		if err := client.SetAccessRequestState(ctx, services.AccessRequestUpdate{
			RequestID: reqID,
			State:     services.RequestState_DENIED,
			Reason:    c.reason,
			Attrs:     attrs,
		}); err != nil {
			return trace.Wrap(err)
		}
	}
	return nil
}

func (c *AccessRequestCommand) Create(client auth.ClientI) error {
	roles := strings.Split(c.roles, ",")
	req, err := services.NewAccessRequest(c.user, roles...)
	if err != nil {
		return trace.Wrap(err)
	}
	if err := client.CreateAccessRequest(context.TODO(), req); err != nil {
		return trace.Wrap(err)
	}
	fmt.Printf("%s\n", req.GetName())
	return nil
}

func (c *AccessRequestCommand) Delete(client auth.ClientI) error {
	for _, reqID := range strings.Split(c.reqIDs, ",") {
		if err := client.DeleteAccessRequest(context.TODO(), reqID); err != nil {
			return trace.Wrap(err)
		}
	}
	return nil
}

// PrintAccessRequests prints access requests
func (c *AccessRequestCommand) PrintAccessRequests(client auth.ClientI, reqs []services.AccessRequest, format string) error {
	switch format {
	case teleport.Text:
		table := asciitable.MakeTable([]string{"Token", "Requestor", "Metadata", "Created At (UTC)", "Status"})
		now := time.Now()
		for _, req := range reqs {
			if now.After(req.GetAccessExpiry()) {
				continue
			}
			params := fmt.Sprintf("roles=%s", strings.Join(req.GetRoles(), ","))
			table.AddRow([]string{
				req.GetName(),
				req.GetUser(),
				params,
				req.GetCreationTime().Format(time.RFC822),
				req.GetState().String(),
			})
		}
		_, err := table.AsBuffer().WriteTo(os.Stdout)
		return trace.Wrap(err)
	case teleport.JSON:
		out, err := json.MarshalIndent(reqs, "", "  ")
		if err != nil {
			return trace.Wrap(err, "failed to marshal requests")
		}
		fmt.Printf("%s\n", out)
		return nil
	default:
		return trace.BadParameter("unknown format %q, must be one of [%q, %q]", format, teleport.Text, teleport.JSON)
	}
}
