package authentication

import "github.com/Rahmannugar/livdot/internal/infra/openapi"

// RegisterContract registers the authentication routes and their results.
func RegisterContract(registry *openapi.Registry) {
	registry.Component("AuthResult", map[string]any{
		"type":     "object",
		"required": []string{"accountId", "email", "token", "expiresAt"},
		"properties": map[string]any{
			"accountId": map[string]any{"type": "string", "example": "018f2c1e-6f2a-7c3b-9d4e-2b8a1c5f7e90"},
			"email":     map[string]any{"type": "string", "format": "email", "example": "host@livdot.local"},
			"token":     map[string]any{"type": "string", "example": "9OxBfUR6ipBj04CHxoCHQoWZxn_-ahSio7kMGWYM4Hg"},
			"expiresAt": map[string]any{"type": "string", "format": "date-time", "example": "2026-09-24T18:00:00Z"},
		},
	})
	result := openapi.Ref("AuthResult")

	registry.Add(openapi.Operation{
		Method: "post", Path: "/api/signup/host", OperationID: "signupHost",
		Summary: "Register a host account", Tag: "authentication",
		Request: hostSignUpRequest{}, RequestName: "HostSignUpRequest",
		Responses: map[string]any{
			"201": result, "400": openapi.Ref("Error"),
			"409": openapi.Ref("Error"), "429": openapi.Ref("Error"),
		},
	})
	registry.Add(openapi.Operation{
		Method: "post", Path: "/api/signup/crew", OperationID: "signupCrew",
		Summary: "Register a crew account", Tag: "authentication",
		Request: crewSignUpRequest{}, RequestName: "CrewSignUpRequest",
		Responses: map[string]any{
			"201": result, "400": openapi.Ref("Error"),
			"409": openapi.Ref("Error"), "429": openapi.Ref("Error"),
		},
	})
	registry.Add(openapi.Operation{
		Method: "post", Path: "/api/signup/user", OperationID: "signupUser",
		Summary: "Register a viewer account", Tag: "authentication",
		Request: userSignUpRequest{}, RequestName: "UserSignUpRequest",
		Responses: map[string]any{
			"201": result, "400": openapi.Ref("Error"),
			"409": openapi.Ref("Error"), "429": openapi.Ref("Error"),
		},
	})

	signIn := func(id, path, summary string) {
		registry.Add(openapi.Operation{
			Method: "post", Path: path, OperationID: id, Summary: summary, Tag: "authentication",
			Request: signInRequest{}, RequestName: "SignInRequest",
			Responses: map[string]any{
				"200": result, "400": openapi.Ref("Error"),
				"401": openapi.Ref("Error"), "429": openapi.Ref("Error"),
			},
		})
	}
	signIn("signinHost", "/api/signin/host", "Sign in a host")
	signIn("signinCrew", "/api/signin/crew", "Sign in a crew")
	signIn("signinUser", "/api/signin/user", "Sign in a viewer")
	signIn("signinInternal", "/api/signin/internal", "Sign in an internal administrator")

	registry.Add(openapi.Operation{
		Method: "post", Path: "/api/signout", OperationID: "signout",
		Summary: "Revoke the presented session token", Tag: "authentication",
		Security: true,
		Responses: map[string]any{
			"204": nil, "401": openapi.Ref("Error"), "429": openapi.Ref("Error"),
		},
	})
}
