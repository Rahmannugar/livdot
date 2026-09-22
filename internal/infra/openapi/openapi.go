// Package openapi builds an OpenAPI 3 document. Request schemas are derived from
// the same Go request structs the handlers bind, so the contract cannot drift
// from runtime validation. Response schemas are supplied explicitly by hand.
package openapi

import (
	"encoding/json"
	"reflect"
	"sort"
	"time"
)

// Operation is one documented route.
type Operation struct {
	Method      string
	Path        string
	OperationID string
	Summary     string
	Tag         string
	Security    bool
	Roles       []string
	Request     any
	RequestName string
	Responses   map[string]any
}

// Registry collects operations and shared response components.
type Registry struct {
	operations []Operation
	components map[string]any
}

func NewRegistry() *Registry {
	return &Registry{components: map[string]any{}}
}

func (registry *Registry) Add(operation Operation) {
	registry.operations = append(registry.operations, operation)
}

// Component registers a named, hand-maintained response schema.
func (registry *Registry) Component(name string, schema map[string]any) {
	registry.components[name] = schema
}

// Document renders the registry to an OpenAPI 3.0 document. Map keys marshal in
// sorted order, so the output is deterministic and stale generation is obvious.
func (registry *Registry) Document(title, version, description string) map[string]any {
	paths := map[string]map[string]any{}
	for _, operation := range registry.operations {
		path, ok := paths[operation.Path]
		if !ok {
			path = map[string]any{}
			paths[operation.Path] = path
		}
		path[operation.Method] = registry.operation(operation)
	}

	components := map[string]any{}
	if len(registry.components) > 0 {
		components["schemas"] = registry.components
	}
	components["securitySchemes"] = map[string]any{
		"bearerAuth": map[string]any{
			"type": "http", "scheme": "bearer", "description": "Opaque session token",
		},
	}

	return map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       title,
			"version":     version,
			"description": description,
		},
		"servers": []any{
			map[string]any{"url": "/"},
		},
		"paths":      paths,
		"components": components,
	}
}

func (registry *Registry) operation(operation Operation) map[string]any {
	entry := map[string]any{
		"operationId": operation.OperationID,
		"summary":     operation.Summary,
		"tags":        []string{operation.Tag},
		"responses":   registry.responses(operation),
	}
	if operation.Request != nil {
		name := operation.RequestName
		registry.components[name] = RequestSchema(operation.Request)
		entry["requestBody"] = map[string]any{
			"required": true,
			"content": map[string]any{
				"application/json": map[string]any{
					"schema": Ref(name),
				},
			},
		}
	}
	parameters := pathParameters(operation)
	if len(parameters) > 0 {
		entry["parameters"] = parameters
	}
	if operation.Security {
		entry["security"] = []any{map[string]any{"bearerAuth": []string{}}}
	}
	if len(operation.Roles) > 0 {
		entry["x-roles"] = operation.Roles
	}
	return entry
}

func (registry *Registry) responses(operation Operation) map[string]any {
	responses := map[string]any{}
	for status, schema := range operation.Responses {
		responses[status] = map[string]any{
			"description": responseDescription(status),
			"content": map[string]any{
				"application/json": map[string]any{"schema": schema},
			},
		}
	}
	return responses
}

func responseDescription(status string) string {
	switch status {
	case "200", "201", "202":
		return "success"
	case "400":
		return "invalid request"
	case "401":
		return "unauthenticated"
	case "403":
		return "forbidden"
	case "404":
		return "not found"
	case "409":
		return "conflict"
	case "429":
		return "rate limited"
	default:
		return "error"
	}
}

func pathParameters(operation Operation) []any {
	var parameters []any
	for _, segment := range splitPath(operation.Path) {
		if len(segment) > 1 && segment[0] == '{' {
			parameters = append(parameters, map[string]any{
				"name":     segment[1 : len(segment)-1],
				"in":       "path",
				"required": true,
				"schema":   map[string]any{"type": "string"},
			})
		}
	}
	return parameters
}

func splitPath(path string) []string {
	var segments []string
	current := ""
	for _, r := range path {
		if r == '/' {
			if current != "" {
				segments = append(segments, current)
			}
			current = ""
			continue
		}
		current += string(r)
	}
	if current != "" {
		segments = append(segments, current)
	}
	return segments
}

// Ref points at a registered component schema.
func Ref(name string) map[string]any {
	return map[string]any{"$ref": "#/components/schemas/" + name}
}

// RequestSchema reflects a request struct into an object schema. A field is
// required when it is not a pointer and not omitempty, which matches how the
// handlers distinguish set from unset.
func RequestSchema(sample any) map[string]any {
	value := reflect.TypeOf(sample)
	if value.Kind() == reflect.Ptr {
		value = value.Elem()
	}
	properties := map[string]any{}
	var required []string
	for i := 0; i < value.NumField(); i++ {
		field := value.Field(i)
		name, optional := jsonName(field)
		if name == "" {
			continue
		}
		properties[name] = fieldSchema(field.Type)
		if !optional && field.Type.Kind() != reflect.Ptr {
			required = append(required, name)
		}
	}
	sort.Strings(required)
	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func jsonName(field reflect.StructField) (string, bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", false
	}
	name := tag
	optional := false
	if index := indexByte(tag, ','); index >= 0 {
		name = tag[:index]
		optional = contains(tag[index+1:], "omitempty")
	}
	if name == "" {
		name = field.Name
	}
	return name, optional
}

func fieldSchema(field reflect.Type) map[string]any {
	if field == reflect.TypeOf(json.RawMessage{}) {
		return map[string]any{}
	}
	if field == reflect.TypeOf(time.Time{}) {
		return map[string]any{"type": "string", "format": "date-time"}
	}
	switch field.Kind() {
	case reflect.Ptr:
		return fieldSchema(field.Elem())
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return map[string]any{"type": "integer", "format": "int64"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice:
		if field.Elem().Kind() == reflect.Uint8 {
			return map[string]any{}
		}
		return map[string]any{"type": "array", "items": fieldSchema(field.Elem())}
	default:
		return map[string]any{}
	}
}

func indexByte(value string, target byte) int {
	for i := 0; i < len(value); i++ {
		if value[i] == target {
			return i
		}
	}
	return -1
}

func contains(value, substring string) bool {
	if substring == "" {
		return true
	}
	for i := 0; i+len(substring) <= len(value); i++ {
		if value[i:i+len(substring)] == substring {
			return true
		}
	}
	return false
}
