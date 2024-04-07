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
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	sonatypeiq "github.com/sonatype-nexus-community/nexus-iq-api-client-go"
)

// applicationRoleMembershipResource is the resource implementation.
type applicationRoleMembershipResource struct {
	baseResource
}

type applicationRoleMembershipModelResource struct {
	ID            types.String `tfsdk:"id"`
	RoleId        types.String `tfsdk:"role_id"`
	ApplicationId types.String `tfsdk:"application_id"`
	UserName      types.String `tfsdk:"user_name"`
	GroupName     types.String `tfsdk:"group_name"`
}

// NewApplicationRoleMembershipResource is a helper function to simplify the provider implementation.
func NewApplicationRoleMembershipResource() resource.Resource {
	return &applicationRoleMembershipResource{}
}

// Metadata returns the resource type name.
func (r *applicationRoleMembershipResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_application_role_membership"
}

// Schema defines the schema for the resource.
func (r *applicationRoleMembershipResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
			},
			"role_id": schema.StringAttribute{
				Required: true,
			},
			"application_id": schema.StringAttribute{
				Required: true,
			},
			"user_name": schema.StringAttribute{
				Optional: true,
			},
			"group_name": schema.StringAttribute{
				Optional: true,
			},
		},
	}
}

func (r *applicationRoleMembershipResource) ConfigValidators(ctx context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.ExactlyOneOf(
			path.MatchRoot("user_name"),
			path.MatchRoot("group_name"),
		),
	}
}

// Create creates the resource and sets the initial Terraform state.
func (r *applicationRoleMembershipResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data applicationRoleMembershipModelResource

	// Read Terraform plan data into the model
	diags := req.Plan.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Call API to create application role membership
	ctx = context.WithValue(
		ctx,
		sonatypeiq.ContextBasicAuth,
		r.auth,
	)

	// Determine the member type, which can be any of group or user.
	// The resource validator makes sure that exactly one of these is configured.
	var memberType, memberName string
	if !data.GroupName.IsNull() {
		memberType = "group"
		memberName = data.GroupName.ValueString()
	} else {
		memberType = "user"
		memberName = data.UserName.ValueString()
	}

	apiRequest := r.client.RoleMembershipsAPI.GrantRoleMembershipApplicationOrOrganization(ctx, "application", data.ApplicationId.ValueString(), data.RoleId.ValueString(), memberType, memberName)
	apiResponse, err := r.client.RoleMembershipsAPI.GrantRoleMembershipApplicationOrOrganizationExecute(apiRequest)

	// Call API
	if err != nil {
		error_body, _ := io.ReadAll(apiResponse.Body)
		resp.Diagnostics.AddError(
			"Error creating application role membership",
			"Could not create application role membership, unexpected error: "+apiResponse.Status+": "+string(error_body),
		)
		return
	}

	// Map response body to schema and populate Computed attribute values.
	// Because the application role membership does not have an ID of its own, we create a synthetic one based on the provided attributes.
	data.ID = types.StringValue(fmt.Sprintf("%s_%s_%s_%s", data.ApplicationId.ValueString(), data.RoleId.ValueString(), memberType, memberName))

	// Set state to fully populated data
	diags = resp.State.Set(ctx, data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Read refreshes the Terraform state with the latest data.
func (r *applicationRoleMembershipResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data applicationRoleMembershipModelResource

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	ctx = context.WithValue(
		ctx,
		sonatypeiq.ContextBasicAuth,
		r.auth,
	)

	// Get refreshed application role membership from IQ
	apiRequest := r.client.RoleMembershipsAPI.GetRoleMembershipsApplicationOrOrganization(ctx, "application", data.ApplicationId.ValueString())
	roleMemberships, apiResponse, err := r.client.RoleMembershipsAPI.GetRoleMembershipsApplicationOrOrganizationExecute(apiRequest)

	// Check if we received a list of role mappings.
	if err != nil {
		if apiResponse.StatusCode == http.StatusNotFound {
			resp.State.RemoveResource(ctx)
		} else {
			resp.Diagnostics.AddError(
				"Error Reading IQ application role membership",
				"Could not read applicaton role membership with ID "+data.ID.ValueString()+": "+err.Error(),
			)
		}
		return
	}

	// Determine the member type, which can be any of group or user.
	// The resource validator makes sure that exactly one of these is configured.
	var memberType, memberName string
	if !data.GroupName.IsNull() {
		memberType = "GROUP"
		memberName = data.GroupName.ValueString()
	} else {
		memberType = "USER"
		memberName = data.UserName.ValueString()
	}

	// Find our application role membership mapping
	var applicationRoleMembership *sonatypeiq.ApiMemberDTO
	for _, roleMembership := range roleMemberships.MemberMappings {
		if *roleMembership.RoleId == data.RoleId.ValueString() {
			for _, member := range roleMembership.Members {
				if *member.Type == memberType && *member.UserOrGroupName == memberName && *member.OwnerType == "APPLICATION" && *member.OwnerId == data.ApplicationId.ValueString() {
					applicationRoleMembership = &member
				}
			}
		}
	}

	if applicationRoleMembership == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	// Set refreshed state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// // Update updates the resource and sets the updated Terraform state on success.
//
//	func (r *applicationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
//		var plan applicationModelResource
//		var state applicationModelResource
//		resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
//		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
//		if resp.Diagnostics.HasError() {
//			return
//		}
//
//		// Make Update API Call
//		ctx = context.WithValue(
//			ctx,
//			sonatypeiq.ContextBasicAuth,
//			r.auth,
//		)
//		app_update_request := r.client.ApplicationsAPI.UpdateApplication(ctx, state.ID.ValueString())
//		app_update_request = app_update_request.ApiApplicationDTO(sonatypeiq.ApiApplicationDTO{
//			Name:            plan.Name.ValueStringPointer(),
//			PublicId:        plan.PublicId.ValueStringPointer(),
//			OrganizationId:  plan.OrganizationId.ValueStringPointer(),
//			ContactUserName: plan.ContactUserName.ValueStringPointer(),
//		})
//
//		application, api_response, err := app_update_request.Execute()
//
//		// Call API
//		if err != nil {
//			error_body, _ := io.ReadAll(api_response.Body)
//			resp.Diagnostics.AddError(
//				"Error updating Application",
//				"Could not update Application, unexpected error: "+api_response.Status+": "+string(error_body),
//			)
//			return
//		}
//
//		// Map response body to schema and populate Computed attribute values
//		plan.ID = types.StringValue(*application.Id)
//		plan.LastUpdated = types.StringValue(time.Now().Format(time.RFC850))
//
//		// Set state to fully populated data
//		resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
//		if resp.Diagnostics.HasError() {
//			return
//		}
//
// }
//
// Delete deletes the resource and removes the Terraform state on success.
func (r *applicationRoleMembershipResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data applicationRoleMembershipModelResource
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Make Delete API Call
	ctx = context.WithValue(
		ctx,
		sonatypeiq.ContextBasicAuth,
		r.auth,
	)

	// Determine the member type, which can be any of group or user.
	// The resource validator makes sure that exactly one of these is configured.
	var memberType, memberName string
	if !data.GroupName.IsNull() {
		memberType = "group"
		memberName = data.GroupName.ValueString()
	} else {
		memberType = "user"
		memberName = data.UserName.ValueString()
	}

	apiRequest := r.client.RoleMembershipsAPI.RevokeRoleMembershipApplicationOrOrganization(ctx, "application", data.ApplicationId.ValueString(), data.RoleId.ValueString(), memberType, memberName)
	apiResponse, err := r.client.RoleMembershipsAPI.RevokeRoleMembershipApplicationOrOrganizationExecute(apiRequest)
	if err != nil {
		error_body, _ := io.ReadAll(apiResponse.Body)
		resp.Diagnostics.AddError(
			"Error deleting application role membership",
			"Could not delete application role membership, unexpected error: "+apiResponse.Status+": "+string(error_body),
		)
		return
	}
}
