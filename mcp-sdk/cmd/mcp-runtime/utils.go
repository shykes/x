package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"mcp-runtime/internal/dagger"

	"github.com/vektah/gqlparser/gqlerror"
)

type Handler interface {
	Dispatch(context.Context, *Call) (any, error)
}

func Serve(ctx context.Context, handler Handler) error {
	call, err := CurrentCall(ctx)
	if err != nil {
		return err
	}
	result, err := handler.Dispatch(ctx, call)
	if err != nil {
		return call.ReturnError(ctx, err)
	}
	return call.ReturnValue(ctx, result)
}

func CurrentCall(ctx context.Context) (*Call, error) {
	fnCall := dag.CurrentFunctionCall()
	call := Call{
		fnCall: fnCall,
	}
	if moduleName, err := dag.CurrentModule().Name(ctx); err != nil {
		return nil, err
	} else {
		call.ModuleName = moduleName
	}
	if name, err := fnCall.Name(ctx); err != nil {
		return nil, err
	} else {
		call.Name = name
	}
	if parentName, err := fnCall.ParentName(ctx); err != nil {
		return nil, err
	} else {
		call.ParentName = parentName
	}
	if args, err := fnCall.InputArgs(ctx); err != nil {
		return nil, err
	} else {
		call.args = map[string][]byte{}
		for _, arg := range args {
			name, err := arg.Name(ctx)
			if err != nil {
				return nil, err
			}
			value, err := arg.Value(ctx)
			if err != nil {
				return nil, err
			}
			call.args[name] = []byte(value)
		}
	}
	return &call, nil
}

// A developer-friendly representation of a dagger function call
type Call struct {
	fnCall     *dagger.FunctionCall
	ParentName string
	Name       string
	args       map[string][]byte
	ModuleName string
}

// Return true if the call is to the module's constructor
func (call *Call) IsConstructor() bool {
	return call.IsMainObject() && call.Name == ""

}

// Return true if the call is to initialize the module
func (call *Call) IsInit() bool {
	return call.ParentName == ""
}

// Return true if the call is to a function of the module's main object
func (call *Call) IsMainObject() bool {
	return call.ParentName == call.ModuleName
}

func (call *Call) ReturnValue(ctx context.Context, result any) error {
	resultBytes, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := call.fnCall.ReturnValue(ctx, dagger.JSON(resultBytes)); err != nil {
		return fmt.Errorf("store return value: %w", err)
	}
	return nil
}

func (call *Call) ReturnError(ctx context.Context, err error) error {
	var execErr *dagger.ExecError
	if errors.As(err, &execErr) {
		err = execErr.Unwrap()
	}
	panic(err)
	// FIXME
	// return call.fnCall.ReturnError(ctx, dag.Error(unwrapError(err)))
}

func (call *Call) JSONArgs() ([]byte, error) {
	panic("FIXME")
}

func (call *Call) DirectoryArg(name string) (*dagger.Directory, error) {
	data, ok := call.args[name]
	if !ok {
		// FIXME: we are hardcoding an empty directory as default value
		// AND assuming that required arguments are enforced upstream
		return dag.Directory(), nil
	}
	if data == nil {
		return dag.Directory(), nil
	}
	var id dagger.DirectoryID
	if err := json.Unmarshal(data, &id); err != nil {
		return nil, err
	}
	return dag.LoadDirectoryFromID(id), nil
}

func (call *Call) StringArg(name string) (string, error) {
	data, ok := call.args[name]
	if !ok {
		return "", fmt.Errorf("arg not found: %q", name)
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return "", err
	}
	return s, nil
}

// Utility function during module invocation when an error it returned.
func unwrapError(rerr error) string {
	var gqlErr *gqlerror.Error
	if errors.As(rerr, &gqlErr) {
		return gqlErr.Message
	}

	return rerr.Error()
}
