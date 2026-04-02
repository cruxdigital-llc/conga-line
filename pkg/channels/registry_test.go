package channels

import "testing"

// testChannel is a minimal Channel for testing the registry.
type testChannel struct{ name string }

func (t *testChannel) Name() string                          { return t.name }
func (t *testChannel) ValidateBinding(string, string) error  { return nil }
func (t *testChannel) SharedSecrets() []SecretDef            { return nil }
func (t *testChannel) HasCredentials(map[string]string) bool { return false }
func (t *testChannel) OpenClawChannelConfig(string, ChannelBinding, map[string]string) (map[string]any, error) {
	return nil, nil
}
func (t *testChannel) OpenClawPluginConfig(bool) map[string]any                          { return nil }
func (t *testChannel) RoutingEntries(string, ChannelBinding, string, int) []RoutingEntry { return nil }
func (t *testChannel) AgentEnvVars(map[string]string) map[string]string                  { return nil }
func (t *testChannel) RouterEnvVars(map[string]string) map[string]string                 { return nil }
func (t *testChannel) WebhookPath() string                                               { return "" }
func (t *testChannel) BehaviorTemplateVars(string, ChannelBinding) map[string]string     { return nil }

func TestRegisterAndGet(t *testing.T) {
	// Save and restore registry state
	orig := registered
	registered = map[string]Channel{}
	defer func() { registered = orig }()

	ch := &testChannel{name: "test-platform"}
	Register(ch)

	got, ok := Get("test-platform")
	if !ok {
		t.Fatal("Get(test-platform) returned false")
	}
	if got.Name() != "test-platform" {
		t.Errorf("Name() = %q, want test-platform", got.Name())
	}

	_, ok = Get("nonexistent")
	if ok {
		t.Error("Get(nonexistent) should return false")
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	orig := registered
	registered = map[string]Channel{}
	defer func() { registered = orig }()

	Register(&testChannel{name: "dup"})

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate registration")
		}
	}()
	Register(&testChannel{name: "dup"})
}

func TestParseBinding(t *testing.T) {
	orig := registered
	registered = map[string]Channel{}
	defer func() { registered = orig }()

	Register(&testChannel{name: "slack"})

	tests := []struct {
		input    string
		wantPlat string
		wantID   string
		wantErr  bool
	}{
		{"slack:U0123456789", "slack", "U0123456789", false},
		{"slack:C9876543210", "slack", "C9876543210", false},
		{"slack:", "slack", "", false}, // empty ID is valid at parse level
		{"nocolon", "", "", true},
		{"unknown:id", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			b, err := ParseBinding(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseBinding(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if b.Platform != tt.wantPlat {
				t.Errorf("Platform = %q, want %q", b.Platform, tt.wantPlat)
			}
			if b.ID != tt.wantID {
				t.Errorf("ID = %q, want %q", b.ID, tt.wantID)
			}
		})
	}
}

func TestAll(t *testing.T) {
	orig := registered
	registered = map[string]Channel{}
	defer func() { registered = orig }()

	Register(&testChannel{name: "a"})
	Register(&testChannel{name: "b"})

	all := All()
	if len(all) != 2 {
		t.Errorf("All() returned %d channels, want 2", len(all))
	}
}
