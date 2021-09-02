package action

import (
	"fmt"

	"github.com/takescoop/terraform-cloud-workspace-action/internal/tfeprovider"
)

type VariablesInput []VariablesInputItem

type WorkspaceVariablesInput map[string]VariablesInput

type VariablesInputItem struct {
	Key         string `yaml:"key"`
	Value       string `yaml:"value"`
	Description string `yaml:"description,omitempty"`
	Category    string `yaml:"category,omitempty"`
	Sensitive   bool   `yaml:"sensitive,omitempty"`
}

type Variables []Variable

type Variable struct {
	Key         string
	Value       string
	Description string
	Category    string
	Sensitive   bool
	Workspace   *Workspace
}

// NewVariable creates a new Variable struct
func NewVariable(vi VariablesInputItem, w *Workspace) *Variable {
	return &Variable{
		Key:         vi.Key,
		Value:       vi.Value,
		Description: vi.Description,
		Category:    vi.Category,
		Sensitive:   vi.Sensitive,
		Workspace:   w,
	}
}

// ToResource converts a variable to a Terraform variable resource
func (v Variable) ToResource() *tfeprovider.Variable {
	return &tfeprovider.Variable{
		Key:         v.Key,
		Value:       v.Value,
		Description: v.Description,
		Category:    v.Category,
		Sensitive:   v.Sensitive,
		WorkspaceID: fmt.Sprintf("${tfe_workspace.workspace[%q].id}", v.Workspace.Name),
	}
}

// ToResource converts a list of variables to a list of Terraform variable resources
func (vs Variables) ToResource() []*tfeprovider.Variable {
	vars := make([]*tfeprovider.Variable, len(vs))

	for i, v := range vs {
		vars[i] = v.ToResource()
	}

	return vars
}
