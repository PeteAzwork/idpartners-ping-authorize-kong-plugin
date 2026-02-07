package main

import "context"

// PolicyProvider abstracts the sideband communication protocol.
// The initial implementation is PingAuthorize Sideband API.
// Future implementations may include AuthZen standard APIs.
type PolicyProvider interface {
	// EvaluateRequest sends the client request for policy evaluation (access phase).
	EvaluateRequest(ctx context.Context, req *SidebandAccessRequest) (*SidebandAccessResponse, error)

	// EvaluateResponse sends the upstream response for final evaluation (response phase).
	EvaluateResponse(ctx context.Context, req *SidebandResponsePayload) (*SidebandResponseResult, error)
}
