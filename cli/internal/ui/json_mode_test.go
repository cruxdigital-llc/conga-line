package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetJSONMode_InlineJSON(t *testing.T) {
	defer ResetJSONMode()

	err := SetJSONMode(`{"name":"test","port":8080,"active":true}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !JSONInputActive {
		t.Error("expected JSONInputActive to be true")
	}
	if !OutputJSON {
		t.Error("expected OutputJSON to be true")
	}

	s, ok := GetString("name")
	if !ok || s != "test" {
		t.Errorf("GetString(name) = %q, %v; want %q, true", s, ok, "test")
	}

	i, ok := GetInt("port")
	if !ok || i != 8080 {
		t.Errorf("GetInt(port) = %d, %v; want 8080, true", i, ok)
	}

	b, ok := GetBool("active")
	if !ok || !b {
		t.Errorf("GetBool(active) = %v, %v; want true, true", b, ok)
	}
}

func TestSetJSONMode_FileRef(t *testing.T) {
	defer ResetJSONMode()

	dir := t.TempDir()
	path := filepath.Join(dir, "input.json")
	if err := os.WriteFile(path, []byte(`{"key":"from_file"}`), 0644); err != nil {
		t.Fatal(err)
	}

	err := SetJSONMode("@" + path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s, ok := GetString("key")
	if !ok || s != "from_file" {
		t.Errorf("GetString(key) = %q, %v; want %q, true", s, ok, "from_file")
	}
}

func TestSetJSONMode_MalformedJSON(t *testing.T) {
	defer ResetJSONMode()

	err := SetJSONMode(`{bad`)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestSetJSONMode_EmptyString(t *testing.T) {
	defer ResetJSONMode()

	err := SetJSONMode("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if JSONInputActive {
		t.Error("expected JSONInputActive to be false for empty string")
	}
}

func TestSetJSONMode_FileNotFound(t *testing.T) {
	defer ResetJSONMode()

	err := SetJSONMode("@/nonexistent/path.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestGetString_Missing(t *testing.T) {
	defer ResetJSONMode()
	_ = SetJSONMode(`{"other":"val"}`)

	s, ok := GetString("missing")
	if ok || s != "" {
		t.Errorf("GetString(missing) = %q, %v; want %q, false", s, ok, "")
	}
}

func TestGetString_WrongType(t *testing.T) {
	defer ResetJSONMode()
	_ = SetJSONMode(`{"num":42}`)

	s, ok := GetString("num")
	if ok || s != "" {
		t.Errorf("GetString(num) = %q, %v; want %q, false", s, ok, "")
	}
}

func TestGetString_NilData(t *testing.T) {
	defer ResetJSONMode()
	ResetJSONMode()

	s, ok := GetString("any")
	if ok || s != "" {
		t.Errorf("GetString on nil data = %q, %v; want %q, false", s, ok, "")
	}
}

func TestGetInt_Missing(t *testing.T) {
	defer ResetJSONMode()
	_ = SetJSONMode(`{"name":"test"}`)

	i, ok := GetInt("missing")
	if ok || i != 0 {
		t.Errorf("GetInt(missing) = %d, %v; want 0, false", i, ok)
	}
}

func TestGetInt_WrongType(t *testing.T) {
	defer ResetJSONMode()
	_ = SetJSONMode(`{"name":"test"}`)

	i, ok := GetInt("name")
	if ok || i != 0 {
		t.Errorf("GetInt(name) = %d, %v; want 0, false", i, ok)
	}
}

func TestGetBool_Missing(t *testing.T) {
	defer ResetJSONMode()
	_ = SetJSONMode(`{"name":"test"}`)

	b, ok := GetBool("missing")
	if ok || b {
		t.Errorf("GetBool(missing) = %v, %v; want false, false", b, ok)
	}
}

func TestGetBool_WrongType(t *testing.T) {
	defer ResetJSONMode()
	_ = SetJSONMode(`{"name":"test"}`)

	b, ok := GetBool("name")
	if ok || b {
		t.Errorf("GetBool(name) = %v, %v; want false, false", b, ok)
	}
}

func TestMustGetString_Present(t *testing.T) {
	defer ResetJSONMode()
	_ = SetJSONMode(`{"key":"val"}`)

	s, err := MustGetString("key")
	if err != nil || s != "val" {
		t.Errorf("MustGetString(key) = %q, %v; want %q, nil", s, err, "val")
	}
}

func TestMustGetString_Missing(t *testing.T) {
	defer ResetJSONMode()
	_ = SetJSONMode(`{"other":"val"}`)

	_, err := MustGetString("missing")
	if err == nil {
		t.Fatal("expected error for missing field")
	}
}

func TestJSONData(t *testing.T) {
	defer ResetJSONMode()
	_ = SetJSONMode(`{"a":"1","b":"2"}`)

	data := JSONData()
	if data == nil {
		t.Fatal("JSONData() returned nil")
	}
	if len(data) != 2 {
		t.Errorf("JSONData() has %d keys; want 2", len(data))
	}
}
