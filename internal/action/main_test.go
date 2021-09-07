package action

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/go-tfe"
	"github.com/stretchr/testify/assert"
	yaml "gopkg.in/yaml.v2"
)

var testWorkspacePrefix string = "action-test"

// newTestInputs returns an Inputs object with test defaults
func newTestInputs(t *testing.T) *Inputs {
	action, err := getActionConfig()
	if err != nil {
		t.Fatal(err)
	}

	imp, err := strconv.ParseBool(action.Inputs["import"].Default)
	if err != nil {
		t.Fatal(err)
	}

	token := os.Getenv("TF_TOKEN")
	if token == "" {
		t.Fatal(`Error: "TF_TOKEN" must be set in the environment`)
	}

	organization := os.Getenv("TF_ORGANIZATION")
	if organization == "" {
		t.Fatal(`Error: "TF_ORGANIZATION" must be set in the environment`)
	}

	return &Inputs{
		Token:                  os.Getenv("tf_token"),
		Organization:           os.Getenv("tf_organization"),
		Host:                   action.Inputs["terraform_host"].Default,
		Name:                   fmt.Sprintf("%s-%d", testWorkspacePrefix, time.Now().Unix()),
		Import:                 imp,
		Apply:                  true,
		TFEProviderVersion:     action.Inputs["tfe_provider_version"].Default,
		RunnerTerraformVersion: action.Inputs["runner_terraform_version"].Default,
		TerraformVersion:       action.Inputs["terraform_version"].Default,
	}
}

// removeTestWorkspacesFunc returns a function that removes all workspaces created by the integration tests
func removeTestWorkspacesFunc(t *testing.T, ctx context.Context, client *tfe.Client) func() {
	return func() {
		removeTestWorkspaces(t, ctx, client)
	}
}

// removeTestWorkspaces deletes all test workspaces created by these tests
func removeTestWorkspaces(t *testing.T, ctx context.Context, client *tfe.Client) {
	workspaces, err := client.Workspaces.List(ctx, "org", tfe.WorkspaceListOptions{
		Search: tfe.String(testWorkspacePrefix),
		ListOptions: tfe.ListOptions{
			PageSize: maxPageSize,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, ws := range workspaces.Items {
		if err := client.Workspaces.DeleteByID(ctx, ws.ID); err != nil {
			t.Fatal(err)
		}
	}
}

type ActionInput struct {
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
	Default     string `yaml:"default"`
}

type ActionConfig struct {
	Name        string                 `yaml:"name"`
	Description string                 `yaml:"description"`
	Inputs      map[string]ActionInput `yaml:"inputs"`
}

// getActionConfig returns the action configuration file
func getActionConfig() (*ActionConfig, error) {
	actionFile, err := ioutil.ReadFile("../../action.yml")
	if err != nil {
		return nil, err
	}

	var action ActionConfig

	if err := yaml.Unmarshal(actionFile, &action); err != nil {
		return nil, err
	}

	return &action, nil
}

func TestCreateWorkspace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()

	config := newTestInputs(t)

	client, err := tfe.NewClient(&tfe.Config{
		Address: fmt.Sprintf("https://%s", config.Host),
		Token:   config.Token,
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(removeTestWorkspacesFunc(t, ctx, client))

	removeTestWorkspaces(t, ctx, client)

	_, err = client.Workspaces.Read(ctx, config.Organization, config.Name)
	if err == nil {
		t.Fatal("workspace should not exist, and an error should be returned")
	}

	if errors.Is(err, tfe.ErrResourceNotFound) {
		t.Fatalf("Error is not workspace not found: %s", err)
	}

	if err = Run(config); err != nil {
		t.Fatal(err)
	}

	ws, err := client.Workspaces.Read(ctx, config.Organization, config.Name)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, ws.Name, config.Name)
}

func TestImportExistingResources(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()

	config := newTestInputs(t)

	config.Variables = `---
- key: foo
  value: baz
  category: terraform`

	client, err := tfe.NewClient(&tfe.Config{
		Address: fmt.Sprintf("https://%s", config.Host),
		Token:   config.Token,
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(removeTestWorkspacesFunc(t, ctx, client))

	removeTestWorkspaces(t, ctx, client)

	ws, err := client.Workspaces.Create(ctx, config.Organization, tfe.WorkspaceCreateOptions{
		Name:             &config.Name,
		TerraformVersion: tfe.String("1.0.4"),
	})
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, ws.TerraformVersion, "1.0.4")

	v, err := client.Variables.Create(ctx, ws.ID, tfe.VariableCreateOptions{
		Key:      tfe.String("foo"),
		Value:    tfe.String("bar"),
		Category: tfe.Category(tfe.CategoryTerraform),
	})
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, v.Value, "bar")

	if err = Run(config); err != nil {
		t.Fatal(err)
	}

	ws, err = client.Workspaces.Read(ctx, config.Organization, config.Name)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, ws.TerraformVersion, "1.0.5")

	v, err = client.Variables.Read(ctx, ws.ID, v.ID)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, v.Value, "baz")
}

func TestDriftCorrection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()

	config := newTestInputs(t)

	client, err := tfe.NewClient(&tfe.Config{
		Address: fmt.Sprintf("https://%s", config.Host),
		Token:   config.Token,
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(removeTestWorkspacesFunc(t, ctx, client))

	removeTestWorkspaces(t, ctx, client)

	ws, err := client.Workspaces.Create(ctx, config.Organization, tfe.WorkspaceCreateOptions{
		Name:             &config.Name,
		TerraformVersion: &config.TerraformVersion,
	})
	if err != nil {
		t.Fatal(err)
	}

	v, err := client.Variables.Create(ctx, ws.ID, tfe.VariableCreateOptions{
		Key:      tfe.String("foo"),
		Value:    tfe.String("bar"),
		Category: tfe.Category(tfe.CategoryTerraform),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err = Run(config); err != nil {
		t.Fatal(err)
	}

	_, err = client.Variables.Read(ctx, ws.ID, v.ID)
	if err == nil {
		t.Fatal("Expected variable not to exist")
	}

	if errors.Is(err, tfe.ErrResourceNotFound) {
		t.Fatalf("Expected error to be resource not found: %s", err)
	}
}

func TestMultipleWorkspaces(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()

	config := newTestInputs(t)

	config.Workspaces = `---
- staging
- production`

	config.WorkspaceVariables = `---
staging:
  - key: environment
    value: staging
    category: env
production:
  - key: environment
    value: production
    category: env`

	client, err := tfe.NewClient(&tfe.Config{
		Address: fmt.Sprintf("https://%s", config.Host),
		Token:   config.Token,
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(removeTestWorkspacesFunc(t, ctx, client))

	removeTestWorkspaces(t, ctx, client)

	ws, err := client.Workspaces.List(ctx, config.Organization, tfe.WorkspaceListOptions{
		Search: &testWorkspacePrefix,
	})
	if err != nil {
		t.Fatal(err)
	}

	assert.Len(t, ws.Items, 0)

	if err = Run(config); err != nil {
		t.Fatal(err)
	}

	ws, err = client.Workspaces.List(ctx, config.Organization, tfe.WorkspaceListOptions{
		Search: &testWorkspacePrefix,
	})
	if err != nil {
		t.Fatal(err)
	}

	assert.Len(t, ws.Items, 2)

	for _, ws := range ws.Items {
		v, err := client.Variables.List(ctx, ws.ID, tfe.VariableListOptions{})
		if err != nil {
			t.Fatal(err)
		}

		assert.Len(t, v.Items, 1)
	}
}
