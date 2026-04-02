package provider

import "errors"

// ErrNotFound indicates a resource (agent, secret, etc.) was not found.
// Provider implementations should wrap this error so callers can check with errors.Is().
var ErrNotFound = errors.New("resource not found")

// ErrBindingExists indicates a channel binding already exists for the agent/platform.
var ErrBindingExists = errors.New("binding already exists")
