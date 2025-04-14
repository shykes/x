package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"dagger.io/dagger"
	"gopkg.in/yaml.v3"
)

func main() {
	agent, err := CurrentAgent(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load agent: %s", err.Error())
		os.Exit(1)
	}
	if err := Serve(ctx, agent); err != nil {
		fmt.Fprintf(os.Stderr, "serve: %s", err.Error())
		os.Exit(2)
	}
}

type Agent struct {
	Name    string    `yaml:"-"`
	Model   string    `yaml:"model"`
	Actions []*Action `yaml:"actions,omitempty"`
}

func CurrentAgent(ctx context.Context) (*Agent, error) {
	configPath := "agent.yaml"
	if workdir := os.Getenv("RUNTIME_WORKDIR"); workdir != "" {
		configPath = workdir + "/" + configPath
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	var cfg Agent
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}
	for _, action := range cfg.Actions {
		fmt.Printf("loaded action %q\n", action.Name)
		action.agent = &cfg
		if action.Model == "" {
			action.Model = cfg.Model
		}
		for _, input := range action.Inputs {
			if input.Typename == "" {
				continue
			}
			input.Typename = strings.ToUpper(input.Typename[0:1]) + strings.ToLower(input.Typename[1:])
		}
		for _, output := range action.Outputs {
			if output.Typename == "" {
				continue
			}
			output.Typename = strings.ToUpper(output.Typename[0:1]) + strings.ToLower(output.Typename[1:])
		}
	}
	moduleName, err := dag.CurrentModule().Name(ctx)
	if err != nil {
		return nil, err
	}
	cfg.Name = moduleName
	return &cfg, nil
}

func (a *Agent) Dispatch(ctx context.Context, call *Call) (any, error) {
	switch call.ParentName {
	case "":
		return a.dispatchEntrypoint(ctx)
	case strings.ToLower(a.Name):
		action, found := a.action(call.Name)
		if !found {
			return nil, fmt.Errorf("undefined action: %q", call.Name)
		}
		return action.Dispatch(ctx, call)
	default:
		break
	}
	return nil, fmt.Errorf("unknown parent object: %s", call.ParentName)
}

// Dispatch the execution of the module's entrypoint
func (a *Agent) dispatchEntrypoint(_ context.Context) (*dagger.Module, error) {
	mod := dag.Module()
	root := dag.TypeDef().WithObject(a.Name)
	for _, action := range a.Actions {
		fmt.Printf("dispatchEntrypoint: %q\n%s", action.Name, action.Dump())
		fn, err := action.Function()
		if err != nil {
			return nil, err
		}
		// Install the function
		root = root.WithFunction(fn)
		// Install the function's return type
		mod = mod.WithObject(fn.ReturnType())
	}
	return mod.WithObject(root), nil
}

func (a *Agent) action(name string) (*Action, bool) {
	for i := range a.Actions {
		if action := a.Actions[i]; action.Name == name {
			return action, true
		}
	}
	return nil, false
}

type Action struct {
	agent       *Agent
	Model       string     `yaml:"model,omitempty"`
	Name        string     `yaml:"name"`
	Description string     `yaml:"description"`
	Inputs      []*Binding `yaml:"inputs,omitempty"`
	Outputs     []*Binding `yaml:"outputs,omitempty"`
}

func (action *Action) Dump() string {
	b, err := json.MarshalIndent(action, "", " ")
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%s\n", b)
}

func (action *Action) Dispatch(ctx context.Context, call *Call) (any, error) {
	llm, err := action.LLM(ctx, call)
	if err != nil {
		return nil, err
	}
	// Execute the agentic loop, and retrieve the modified env
	env := llm.Loop().Env()
	result := map[string]any{}
	for _, output := range action.Outputs {
		val, err := output.OutputValue(env)
		if err != nil {
			return nil, err
		}
		result[output.Name] = val
	}
	return result, nil
}

func (action *Action) LLM(ctx context.Context, call *Call) (*dagger.LLM, error) {
	// Initialize the LLM's environment
	env := dag.Env(dagger.EnvOpts{Privileged: true})
	var err error
	// Bind environment inputs
	for _, input := range action.Inputs {
		env, err = input.BindInput(env, call)
		if err != nil {
			return nil, err
		}
	}
	// Bind environment outputs
	for _, output := range action.Outputs {
		env, err = output.BindOutput(env)
		if err != nil {
			return nil, err
		}
	}
	agent := dag.
		LLM(dagger.LLMOpts{Model: action.Model}).
		WithEnv(env).
		WithPrompt(fmt.Sprintf(`You are %s, a helpful assistant. Your task is: <task>%s</task>
To accomplish your task, you are given access to all the tools you need. Use them, don't tell the user to use them.
When you are finished, use the 'return' tool to return the required outputs to the user.
Take care to read the descriptions of your available tools, as well as the required arguments for the return tool.
They are important to accomplish your task.
Again, your task is: <task>%s</task>`, action.agent.Name, action.Description, action.Description))
	return agent, nil
}

func (action *Action) Function() (*dagger.Function, error) {
	fmt.Printf("building function definition for action %q: %d outputs\n", action.Name, len(action.Outputs))
	// Define the return type
	returnType := dag.TypeDef().WithObject(action.Name + "Result")
	// Each action output is a field in the return type
	for _, output := range action.Outputs {
		fmt.Printf("%q: examining output %q\n", action.Name, output.Name)
		outputType, err := output.Type()
		if err != nil {
			return nil, fmt.Errorf("%s: parse type for output %q: %s", action.Name, output.Name, err.Error())
		}
		returnType = returnType.WithField(
			output.Name,
			outputType,
			dagger.TypeDefWithFieldOpts{
				Description: output.Description,
			},
		)
	}
	// Define the function
	fn := dag.Function(action.Name, returnType).WithDescription(action.Description)
	// Each action input is an argument to the function
	for _, input := range action.Inputs {
		inputType, err := input.Type()
		if err != nil {
			return nil, fmt.Errorf("parse type for input %q: %s", input.Name, err.Error())
		}
		if input.Optional != nil {
			inputType = inputType.WithOptional(*input.Optional)
		}
		fn = fn.WithArg(input.Name, inputType, dagger.FunctionWithArgOpts{
			Description: input.Description,
		})
	}
	return fn, nil
}

type Binding struct {
	Name         string  `yaml:"name"`
	Typename     string  `yaml:"type"`
	Description  string  `yaml:"description"`
	Optional     *bool   `yaml:"optional,omitempty"`
	Instructions *string `yaml:"instructions,omitempty"`
}

// Bind an input to the given environment
func (input Binding) BindInput(env *dagger.Env, call *Call) (*dagger.Env, error) {
	desc := input.Description
	if input.Instructions != nil {
		desc += "\n" + *input.Instructions
	}
	switch input.Typename {
	case "String":
		s, err := call.StringArg(input.Name)
		if err != nil {
			if input.Optional != nil && *input.Optional {
				return env, nil
			}
			return nil, err
		}
		return env.WithStringInput(input.Name, s, desc), nil
	case "Directory":
		dir, err := call.DirectoryArg(input.Name)
		if err != nil {
			if input.Optional != nil && *input.Optional {
				return env, nil
			}
			return nil, err
		}
		return env.WithDirectoryInput(input.Name, dir, desc), nil
	}
	return nil, fmt.Errorf("Unsupported input type: %s", input.Typename)
}

func (output Binding) BindOutput(env *dagger.Env) (*dagger.Env, error) {
	desc := output.Description
	if output.Instructions != nil {
		desc += "\n" + *output.Instructions
	}
	// FIXME: support more than directory
	return env.WithDirectoryOutput(output.Name, desc), nil
}

func (output Binding) OutputValue(env *dagger.Env) (any, error) {
	binding := env.Output(output.Name)
	switch output.Typename {
	case "Directory":
		return binding.AsDirectory().ID(ctx)
	case "File":
		return binding.AsFile().ID(ctx)
	case "Container":
		return binding.AsContainer().ID(ctx)
	}
	return nil, fmt.Errorf("unsupported output type: %s", output.Typename)
}

func (b Binding) Type() (*dagger.TypeDef, error) {
	switch b.Typename {
	case "String":
		return dag.TypeDef().WithKind(dagger.TypeDefKindStringKind), nil
	case "Int", "Integer":
		return dag.TypeDef().WithKind(dagger.TypeDefKindIntegerKind), nil
	case "Bool":
		return dag.TypeDef().WithKind(dagger.TypeDefKindBooleanKind), nil
	case "Directory":
		return dag.TypeDef().WithObject("Directory"), nil
	case "Container":
		return dag.TypeDef().WithObject("Container"), nil
	case "Secret":
		return dag.TypeDef().WithObject("Secret"), nil
	case "Service":
		return dag.TypeDef().WithObject("Service"), nil
	case "Void":
		return dag.TypeDef().WithKind(dagger.TypeDefKindVoidKind), nil
	}
	return nil, fmt.Errorf("unknown type: %q", b.Typename)
}
