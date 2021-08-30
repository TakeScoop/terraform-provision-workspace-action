package action

import (
	"context"
	"fmt"

	tfe "github.com/hashicorp/go-tfe"
	"github.com/hashicorp/terraform-exec/tfexec"
	tfjson "github.com/hashicorp/terraform-json"
	"github.com/sethvargo/go-githubactions"
)

var maxPageSize int = 100

func shouldImport(ctx context.Context, tf TerraformCLI, address string) (bool, error) {
	state, err := tf.Show(ctx)
	if err != nil {
		return false, err
	}

	if state.Values == nil {
		return true, nil
	}

	for _, r := range state.Values.RootModule.Resources {
		if address == r.Address {
			return false, nil
		}
	}

	return true, nil
}

type TerraformCLI interface {
	Show(context.Context, ...tfexec.ShowOption) (*tfjson.State, error)
	Import(context.Context, string, string, ...tfexec.ImportOption) error
}

func ImportWorkspace(ctx context.Context, tf TerraformCLI, client *tfe.Client, name string, organization string, opts ...tfexec.ImportOption) error {
	address := fmt.Sprintf("tfe_workspace.workspace[%q]", name)

	imp, err := shouldImport(ctx, tf, address)
	if err != nil {
		return err
	}

	if !imp {
		githubactions.Infof("Workspace %q already exists in state, skipping import\n", name)
		return nil
	}

	ws, err := GetWorkspace(ctx, client, organization, name)
	if err != nil {
		return err
	}

	if ws == nil {
		githubactions.Infof("Workspace %q not found, skipping import\n", name)
		return nil
	}

	githubactions.Infof("Importing workspace: %s\n", ws.Name)

	err = tf.Import(ctx, address, ws.ID, opts...)
	if err != nil {
		return err
	}

	githubactions.Infof("Successful workspace import: %s\n", ws.Name)

	return nil
}

func fetchVariableByKey(ctx context.Context, client *tfe.Client, key string, workspaceID string, page int) (*tfe.Variable, error) {
	vs, err := client.Variables.List(ctx, workspaceID, tfe.VariableListOptions{
		ListOptions: tfe.ListOptions{
			PageSize: maxPageSize,
		},
	})
	if err != nil {
		return nil, err
	}

	for _, v := range vs.Items {
		if v.Key == key {
			return v, nil
		}
	}

	if vs.NextPage > page {
		return fetchVariableByKey(ctx, client, key, workspaceID, vs.NextPage)
	}

	return nil, nil
}

// GetWorkspace returns the requested workspace, nil if the workspace does not exist, an error for any other issues fetching the workspace
func GetWorkspace(ctx context.Context, client *tfe.Client, organization string, workspace string) (*tfe.Workspace, error) {
	ws, err := client.Workspaces.Read(ctx, organization, workspace)
	if err != nil {
		if err.Error() == "resource not found" {
			return nil, nil
		}

		return nil, err
	}

	return ws, nil
}

func ImportVariable(ctx context.Context, tf TerraformCLI, client *tfe.Client, key string, workspace string, organization string, opts ...tfexec.ImportOption) error {
	address := fmt.Sprintf("tfe_variable.%s-%s", workspace, key)

	imp, err := shouldImport(ctx, tf, address)
	if err != nil {
		return err
	}

	if !imp {
		githubactions.Infof("Variable %q already exists in state, skipping import\n", address)
		return nil
	}

	githubactions.Infof("Importing variable: %q\n", address)

	ws, err := GetWorkspace(ctx, client, organization, workspace)
	if err != nil {
		return err
	}

	if ws == nil {
		githubactions.Infof("Workspace %q not found, skipping import\n", workspace)
		return nil
	}

	v, err := fetchVariableByKey(ctx, client, key, ws.ID, 1)
	if err != nil {
		return err
	}

	if v == nil {
		githubactions.Infof("Variable %q for workspace %q not found, skipping import\n", key, workspace)
		return nil
	}

	importID := fmt.Sprintf("%s/%s/%s", organization, workspace, v.ID)

	err = tf.Import(ctx, address, importID, opts...)
	if err != nil {
		return err
	}

	githubactions.Infof("Variable %q successfully imported\n", importID)

	return nil
}

// ImportTeamAccess imports a team access resource by looking up an existing relation
func ImportTeamAccess(ctx context.Context, tf TerraformCLI, client *tfe.Client, organization string, workspace string, teamName string, opts ...tfexec.ImportOption) error {
	address := fmt.Sprintf("tfe_team_access.teams[\"%s-%s\"]", workspace, teamName)

	imp, err := shouldImport(ctx, tf, address)
	if err != nil {
		return err
	}

	if !imp {
		githubactions.Infof("Team access %q already exists in state, skipping import\n", address)
		return nil
	}

	ws, err := GetWorkspace(ctx, client, organization, workspace)
	if err != nil {
		return err
	}

	if ws == nil {
		githubactions.Infof("Workspace %q not found, skipping import\n", workspace)
		return nil
	}

	githubactions.Infof("Importing team access: %q\n", address)

	teams, err := client.Teams.List(ctx, organization, tfe.TeamListOptions{
		ListOptions: tfe.ListOptions{
			PageSize: 100,
		},
	})
	if err != nil {
		return err
	}

	var team *tfe.Team

	for _, t := range teams.Items {
		if t.Name == teamName {
			team = t
		}
	}

	if team == nil {
		return fmt.Errorf("team %q not found", teamName)
	}

	teamAccess, err := client.TeamAccess.List(ctx, tfe.TeamAccessListOptions{
		WorkspaceID: &ws.ID,
	})
	if err != nil {
		return err
	}

	var teamAccessID string

	for _, access := range teamAccess.Items {
		if access.Team.ID == team.ID {
			teamAccessID = access.ID
		}
	}

	if teamAccessID == "" {
		githubactions.Infof("Team access %q for workspace %q not found, skipping import\n", teamName, workspace)
		return nil
	}

	importID := fmt.Sprintf("%s/%s/%s", organization, workspace, teamAccessID)

	if err = tf.Import(ctx, address, importID, opts...); err != nil {
		return err
	}

	githubactions.Infof("Team access %q successfully imported\n", importID)

	return nil
}
