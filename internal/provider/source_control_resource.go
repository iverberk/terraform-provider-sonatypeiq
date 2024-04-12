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
	"io"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	sonatypeiq "github.com/sonatype-nexus-community/nexus-iq-api-client-go"
)

// organizatonRoleMembershipResource is the resource implementation.
type sourceControlResource struct {
	baseResource
}

type sourceControlModelResource struct {
	ID                              types.String `tfsdk:"id"`
	OrganizationID                  types.String `tfsdk:"organization_id"`
	ApplicationID                   types.String `tfsdk:"application_id"`
	RepositoryURL                   types.String `tfsdk:"repository_url"`
	Token                           types.String `tfsdk:"token"`
	BaseBranch                      types.String `tfsdk:"base_branch"`
	Provider                        types.String `tfsdk:"provider"`
	RemediationPullRequestsEnabled  types.Bool   `tfsdk:"remediation_pull_requests_enabled"`
	PullRequestCommentingEnabled    types.Bool   `tfsdk:"pull_request_commenting_enabled"`
	SourceControlEvaluationsEnabled types.Bool   `tfsdk:"source_control_evaluations_enabled"`
}

// NewSourceControlResource is a helper function to simplify the provider implementation.
func NewSourceControlResource() resource.Resource {
	return &sourceControlResource{}
}

// Metadata returns the resource type name.
func (r *sourceControlResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_source_control"
}

// Schema defines the schema for the resource.
func (r *sourceControlResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
			},
			"organization_id": schema.StringAttribute{
				Optional:    true,
				Description: "The organization ID (mutually exclusive with application_id, one of them is required)",
			},
			"application_id": schema.StringAttribute{
				Optional:    true,
				Description: "The application ID (mutually exclusive with organization_id, one of them is required)",
			},
			"repository_url": schema.StringAttribute{
				Optional:    true,
				Description: "HTTP(S) or SSH URL for the SCM system (only valid for applications)",
			},
			"provider": schema.StringAttribute{
				Optional:    true,
				Description: "The SCM provider (required for the root organization)",
			},
			"token": schema.StringAttribute{
				Optional:    true,
				Description: "The access token for the SCM system (required for the root organization)",
			},
			"base_branch": schema.StringAttribute{
				Optional:    true,
				Description: "The base branch for the repository (required for the root organization)",
			},
			"remediation_pull_requests_enabled": schema.StringAttribute{
				Optional:    true,
				Description: "Set to true to enable the Automated Pull Requests feature",
			},
			"pull_request_commenting_enabled": schema.StringAttribute{
				Optional:    true,
				Description: "Set to true to enable the Pull Request Commenting feature.",
			},
			"source_control_evaluations_enabled": schema.StringAttribute{
				Optional:    true,
				Description: "Set to true to enable Nexus IQ triggered source control evaluations",
			},
		},
	}
}

func (r *sourceControlResource) ConfigValidators(ctx context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.ExactlyOneOf(
			path.MatchRoot("organization_id"),
			path.MatchRoot("application_id"),
		),
	}
}

// Create creates the resource and sets the initial Terraform state.
func (r *sourceControlResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data sourceControlModelResource

	// Read Terraform plan data into the model
	diags := req.Plan.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Call API to create source control entry
	ctx = context.WithValue(
		ctx,
		sonatypeiq.ContextBasicAuth,
		r.auth,
	)

	// Determine the owner type, which can be any of organization or application.
	// The resource validator makes sure that exactly one of these is configured.
	var ownerType, internalOwnerId string
	if !data.ApplicationID.IsNull() {
		ownerType = "application"
		internalOwnerId = data.ApplicationID.ValueString()
	} else {
		ownerType = "organization"
		internalOwnerId = data.OrganizationID.ValueString()
	}

	apiSourceControlDTO := sonatypeiq.ApiSourceControlDTO{
		BaseBranch:                      data.BaseBranch.ValueStringPointer(),
		RepositoryUrl:                   data.RepositoryURL.ValueStringPointer(),
		Token:                           data.Token.ValueStringPointer(),
		Provider:                        data.Provider.ValueStringPointer(),
		EnablePullRequests:              data.PullRequestCommentingEnabled.ValueBoolPointer(),
		RemediationPullRequestsEnabled:  data.RemediationPullRequestsEnabled.ValueBoolPointer(),
		SourceControlEvaluationsEnabled: data.SourceControlEvaluationsEnabled.ValueBoolPointer(),
	}

	apiRequest := r.client.SourceControlAPI.AddSourceControl(ctx, ownerType, internalOwnerId)
	apiRequest.ApiSourceControlDTO(apiSourceControlDTO)

	// apiRequest := r.client.RoleMembershipsAPI.GrantRoleMembershipApplicationOrOrganization(ctx, "organization", data.OrganizationId.ValueString(), data.RoleId.ValueString(), ownerType, memberName)
	dto, apiResponse, err := r.client.SourceControlAPI.AddSourceControlExecute(apiRequest)

	// Call API
	if err != nil {
		error_body, _ := io.ReadAll(apiResponse.Body)
		resp.Diagnostics.AddError(
			"Error creating source control entry",
			"Could not create source control entry, unexpected error: "+apiResponse.Status+": "+string(error_body),
		)
		return
	}

	// Map response body to schema and populate Computed attribute values.
	data.ID = types.StringValue(dto.GetId())

	// Set state to fully populated data
	diags = resp.State.Set(ctx, data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Read refreshes the Terraform state with the latest data.
func (r *sourceControlResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data sourceControlModelResource

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

	// Determine the owner type, which can be any of organization or application.
	// The resource validator makes sure that exactly one of these is configured.
	var ownerType, internalOwnerId string
	if !data.ApplicationID.IsNull() {
		ownerType = "application"
		internalOwnerId = data.ApplicationID.ValueString()
	} else {
		ownerType = "organization"
		internalOwnerId = data.OrganizationID.ValueString()
	}

	// Get refreshed source control entry
	apiRequest := r.client.SourceControlAPI.GetSourceControl1(ctx, ownerType, internalOwnerId)
	dto, apiResponse, err := r.client.SourceControlAPI.GetSourceControl1Execute(apiRequest)

	// Check if we received a source control entry
	if err != nil {
		if apiResponse.StatusCode == http.StatusNotFound {
			resp.State.RemoveResource(ctx)
		} else {
			resp.Diagnostics.AddError(
				"Error Reading source control entry",
				"Could not read source control entry with ID "+data.ID.ValueString()+": "+err.Error(),
			)
		}
		return
	}

	data.ID = types.StringValue(dto.GetId())
	data.RepositoryURL = types.StringValue(dto.GetRepositoryUrl())
	data.Token = types.StringValue(dto.GetToken())
	data.BaseBranch = types.StringValue(dto.GetBaseBranch())
	data.Provider = types.StringValue(dto.GetProvider())
	data.RemediationPullRequestsEnabled = types.BoolValue(dto.GetRemediationPullRequestsEnabled())
	data.PullRequestCommentingEnabled = types.BoolValue(dto.GetPullRequestCommentingEnabled())
	data.SourceControlEvaluationsEnabled = types.BoolValue(dto.GetSourceControlEvaluationsEnabled())

	// Set refreshed state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *sourceControlResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data sourceControlModelResource
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

	// Determine the owner type, which can be any of organization or application.
	// The resource validator makes sure that exactly one of these is configured.
	var ownerType, internalOwnerId string
	if !data.ApplicationID.IsNull() {
		ownerType = "application"
		internalOwnerId = data.ApplicationID.ValueString()
	} else {
		ownerType = "organization"
		internalOwnerId = data.OrganizationID.ValueString()
	}

	apiRequest := r.client.SourceControlAPI.DeleteSourceControl(ctx, ownerType, internalOwnerId)
	apiResponse, err := r.client.SourceControlAPI.DeleteSourceControlExecute(apiRequest)
	if err != nil {
		error_body, _ := io.ReadAll(apiResponse.Body)
		resp.Diagnostics.AddError(
			"Error deleting source control entry",
			"Could not delete source control entry, unexpected error: "+apiResponse.Status+": "+string(error_body),
		)
		return
	}
}
