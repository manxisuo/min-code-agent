// Package tools provides the tool system: interface, registry, workspace sandbox
// and the Phase 2 read-only tools.
package tools

import (
	"context"
	"encoding/json"
)

// JSONSchema is a JSON Schema object for tool parameters.
type JSONSchema map[string]any

// Raw serializes the schema for provider APIs.
func (s JSONSchema) Raw() json.RawMessage {
	if s == nil {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	b, err := json.Marshal(s)
	if err != nil {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	return b
}

// Tool is the stable tool contract. Arguments come from the model and must be
// treated as untrusted input.
type Tool interface {
	Name() string
	Description() string
	Schema() JSONSchema
	Execute(ctx context.Context, args json.RawMessage) (Result, error)
}

// Result is what goes back to the model and into traces.
type Result struct {
	Content string         `json:"content"`
	IsError bool           `json:"is_error"`
	Meta    map[string]any `json:"meta,omitempty"`
}

// Registry holds named tools.
type Registry struct {
	tools map[string]Tool
	order []string
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool; later registrations with the same name replace it.
func (r *Registry) Register(t Tool) {
	if t == nil {
		return
	}
	name := t.Name()
	if _, exists := r.tools[name]; !exists {
		r.order = append(r.order, name)
	}
	r.tools[name] = t
}

// Get returns a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Names returns tool names in registration order.
func (r *Registry) Names() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// Execute looks up and runs a tool. Unknown tools return an error result
// (recoverable for the agent) rather than panicking.
func (r *Registry) Execute(ctx context.Context, name string, args json.RawMessage) (Result, error) {
	t, ok := r.tools[name]
	if !ok {
		return Result{Content: "unknown tool: " + name, IsError: true}, nil
	}
	if args == nil {
		args = json.RawMessage(`{}`)
	}
	return t.Execute(ctx, args)
}
