/*
 * Copyright (c) 2019-present Sonatype, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccSourceControlResource(t *testing.T) {

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: fmt.Sprintf(providerConfig + `
        data "sonatypeiq_organization" "sandbox" {
          name = "Sandbox Organization"
        }

        data "sonatypeiq_application" "sandbox" {
          id = "sandbox-application"
        }

        resource "sonatypeiq_source_control" "test" {
          organization_id = data.sonatypeiq_organization.sandbox.id
        }

        `),
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify application role membership
					resource.TestCheckResourceAttrSet("sonatypeiq_source_control.test", "id"),
					resource.TestCheckResourceAttr("sonatypeiq_source_control.test", "organization_id", ""),
				),
			},
		},
	})
}
