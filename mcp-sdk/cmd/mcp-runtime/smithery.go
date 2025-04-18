// ParseSmitheryCommand reads ./smithery.yaml, evaluates the JS
// `commandFunction`, and returns its {command,args,env} as Go values.
//
// Required deps:
//
//	go get gopkg.in/yaml.v3
//	go get github.com/dop251/goja
package main

import (
	"context"
	"fmt"
	"time"

	"dagger.io/dagger"
	"github.com/dop251/goja"
	"gopkg.in/yaml.v3"
)

// CommandSpec mirrors the JS object returned by commandFunction.
type CommandSpec struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

// ParseSmitheryCommand is safe‑to‑call from init()/main().
func ParseSmitheryCommand(ctx context.Context, f *dagger.File) (*CommandSpec, error) {
	// 1 – read YAML -----------------------------------------------------------
	raw, err := f.Contents(ctx)
	if err != nil {
		return nil, fmt.Errorf("read smithery.yaml: %w", err)
	}

	var y struct {
		StartCommand struct {
			Type            string `yaml:"type"`
			CommandFunction string `yaml:"commandFunction"`
		} `yaml:"startCommand"`
	}
	if err = yaml.Unmarshal([]byte(raw), &y); err != nil {
		return nil, fmt.Errorf("decode yaml: %w", err)
	}
	js := y.StartCommand.CommandFunction
	if js == "" {
		return nil, fmt.Errorf("commandFunction missing")
	}

	// 2 – evaluate JS  --------------------------------------------------------
	vm := goja.New()
	// Kill runaway scripts after 1 s.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() {
		<-ctx.Done()
		vm.Interrupt("timeout")
	}()

	val, err := vm.RunString(fmt.Sprintf("(%s)({})", js)) // call with empty config
	if err != nil {
		return nil, fmt.Errorf("eval commandFunction: %w", err)
	}

	// 3 – convert to Go  ------------------------------------------------------
	obj, ok := val.Export().(map[string]any)
	if !ok {
		return nil, fmt.Errorf("commandFunction did not return an object")
	}

	cs := &CommandSpec{Type: y.StartCommand.Type, Env: map[string]string{}}

	if v, ok := obj["command"].(string); ok {
		cs.Command = v
	} else {
		return nil, fmt.Errorf("command field missing/not string")
	}

	if a, ok := obj["args"].([]any); ok {
		cs.Args = make([]string, len(a))
		for i, v := range a {
			cs.Args[i] = fmt.Sprint(v)
		}
	}

	if e, ok := obj["env"].(map[string]any); ok {
		for k, v := range e {
			cs.Env[k] = fmt.Sprint(v)
		}
	}

	return cs, nil
}
