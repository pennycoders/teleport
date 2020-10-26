/*
Copyright 2020 Gravitational, Inc.

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
	"github.com/gravitational/teleport/lib/utils"

	"github.com/gravitational/trace"

	log "github.com/sirupsen/logrus"
)

// TraitMapping is a mapping that maps a trait to one or
// more teleport roles.
type TraitMapping struct {
	// Trait is a teleport trait name
	Trait string `json:"trait"`
	// Value is trait value to match
	Value string `json:"value"`
	// Roles is a list of static teleport roles to map to
	Roles []string `json:"roles,omitempty"`
}

// TraitMappingSet is a set of trait mappings
type TraitMappingSet []TraitMapping

// TraitsToRoles maps the supplied traits to a list of teleport role names.
func (ms TraitMappingSet) TraitsToRoles(traits map[string][]string) []string {
	var roles []string
	for _, mapping := range ms {
		for traitName, traitValues := range traits {
			if traitName != mapping.Trait {
				continue
			}
		TraitLoop:
			for _, traitValue := range traitValues {
				for _, role := range mapping.Roles {
					outRole, err := utils.ReplaceRegexp(mapping.Value, role, traitValue)
					switch {
					case err != nil:
						if trace.IsNotFound(err) {
							log.Debugf("Failed to match expression %v, replace with: %v input: %v, err: %v", mapping.Value, role, traitValue, err)
						}
						// this trait value clearly did not match, move on to another
						continue TraitLoop
						// skip empty replacement or empty role
					case outRole == "":
					case outRole != "":
						roles = append(roles, outRole)
					}
				}
			}
		}
	}
	return utils.Deduplicate(roles)
}
