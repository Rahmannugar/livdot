package authentication

import "github.com/Rahmannugar/livdot/internal/infra/openapi"

// RegisterContract registers the authentication routes and their hand-maintained results.
func RegisterContract(registry *openapi.Registry) {
	result := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"accountId": map[string]any{"type": "string"},
			"email":     map[string]any{"type": "string"},
			"token":     map[string]any{"type": "string"},
			"expiresAt": map[string]any{"type": "string", "format": "date-time"},
		},
	}
	errors := map[string]any{"400": openapi.Ref("Error"), "409": openapi.Ref("Error"), "429": openapi.Ref("Error")}

	signUp := func(id, path string) {
		responses := map[string]any{"201": result}
		for key, value := range errors {
			responses[key] = value
		}
		registry.Add(openapi.Operation{
			Method: "post", Path: path, OperationID: id,
			Summary: "Register a new account", Tag: "authentication",
			Request: credentialRequest{}, RequestName: id + "Request",
			Responses: responses,
		})
	}
	signIn := func(id, path string) {
		responses := map[string]any{"200": result, "401": openapi.Ref("Error")}
		for key, value := range errors {
			responses[key] = value
		}
		registry.Add(openapi.Operation{
			Method: "post", Path: path, OperationID: id,
			Summary: "Sign in an existing account", Tag: "authentication",
			Request: credentialRequest{}, RequestName: id + "Request",
			Responses: responses,
		})
	}

	signUp("signupHost", "/api/signup/host")
	signUp("signupCrew", "/api/signup/crew")
	signUp("signupUser", "/api/signup/user")
	signIn("signinHost", "/api/signin/host")
	signIn("signinCrew", "/api/signin/crew")
	signIn("signinUser", "/api/signin/user")
	signIn("signinInternal", "/api/signin/internal")
}
