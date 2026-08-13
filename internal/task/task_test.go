package task

import (
	"encoding/json"
	"testing"
)

func TestParseAccelerators(t *testing.T) {
	cases := []struct {
		spec any
		want map[string]int
	}{
		{"A100:4", map[string]int{"A100": 4}},
		{"A100", map[string]int{"A100": 1}},
		{"A100:2,V100", map[string]int{"A100": 2, "V100": 1}},
		{[]any{"T4", "A10:2"}, map[string]int{"T4": 1, "A10": 2}},
		{map[string]any{"A100": 8}, map[string]int{"A100": 8}},
	}
	for _, c := range cases {
		got, err := ParseAccelerators(c.spec)
		if err != nil {
			t.Fatalf("ParseAccelerators(%v): %v", c.spec, err)
		}
		if len(got) != len(c.want) {
			t.Fatalf("ParseAccelerators(%v) = %v, want %v", c.spec, got, c.want)
		}
		for k, v := range c.want {
			if got[k] != v {
				t.Fatalf("ParseAccelerators(%v) = %v, want %v", c.spec, got, c.want)
			}
		}
	}
}

func TestParseRange(t *testing.T) {
	exact := mustRange(t, "8")
	if exact.String() != "8" || exact.Min == nil || exact.Max == nil {
		t.Fatalf("parse exact failed: %v", exact)
	}
	lower := mustRange(t, "8+")
	if lower.String() != "8+" || lower.Min == nil || lower.Max != nil {
		t.Fatalf("parse 8+ failed: %v", lower)
	}
	if !lower.Matches(10) || lower.Matches(7) {
		t.Fatal("8+ should match 10 but not 7")
	}
	upper := mustRange(t, "-8")
	if upper.String() != "-8" || upper.Min != nil || upper.Max == nil {
		t.Fatalf("parse -8 failed: %v", upper)
	}
	rangeVal := mustRange(t, "4-8")
	if rangeVal.String() != "4-8" {
		t.Fatalf("parse 4-8 failed: %v", rangeVal)
	}
	if !rangeVal.Matches(6) || rangeVal.Matches(9) || rangeVal.Matches(3) {
		t.Fatal("4-8 should match 6 but not 3 or 9")
	}
}

func mustRange(t *testing.T, s string) *Range {
	t.Helper()
	r, err := ParseRange(s)
	if err != nil {
		t.Fatalf("ParseRange(%q): %v", s, err)
	}
	return r
}

func TestParseTaskYAML(t *testing.T) {
	data := []byte(`
name: mytrain
num_nodes: 2
resources:
  accelerators: A100:1
  cpus: 8+
  memory: 32+
  disk_size: 100
  use_spot: true
setup: |
  pip install -r requirements.txt
run: |
  python train.py
`)
	ts, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if ts.Name != "mytrain" || ts.NumNodes != 2 {
		t.Fatalf("bad basic fields: %+v", ts)
	}
	if ts.Resources.Accelerators["A100"] != 1 {
		t.Fatalf("bad accelerators: %+v", ts.Resources.Accelerators)
	}
	if ts.Resources.UseSpot == nil || !*ts.Resources.UseSpot {
		t.Fatal("use_spot should be true")
	}
	if ts.Resources.Cpus.String() != "8+" {
		t.Fatalf("bad cpus: %s", ts.Resources.Cpus)
	}
	if ts.Resources.DiskSize.String() != "100" {
		t.Fatalf("bad disk: %s", ts.Resources.DiskSize)
	}
}

func TestParseServiceYAML(t *testing.T) {
	data := []byte(`
name: llm
resources:
  cpus: 2+
service:
  replicas: 3
  port: 8080
  run: python server.py
`)
	ts, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if ts.Service == nil {
		t.Fatal("service should be parsed")
	}
	if ts.Service.Replicas != 3 || ts.Service.Port != 8080 {
		t.Fatalf("bad service: %+v", ts.Service)
	}
}

func TestTaskDefaults(t *testing.T) {
	ts, err := Parse([]byte("run: echo hi\n"))
	if err != nil {
		t.Fatal(err)
	}
	if ts.Name != "task" || ts.NumNodes != 1 {
		t.Fatalf("defaults not applied: %+v", ts)
	}
}

func TestParseCredentialsYAML(t *testing.T) {
	data := []byte(`
name: creds-task
resources:
  cloud: aws
  cpus: 2+
credentials:
  aws:
    access_key_id: AKIA123
    secret_access_key: secret123
    region: us-east-1
  aliyun:
    access_key_id: LTAI123
    access_key_secret: alisecret
run: echo hi
`)
	ts, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if ts.Credentials == nil {
		t.Fatal("credentials should be parsed")
	}
	aws := ts.Credentials.ForCloud("aws")
	if aws == nil || aws.AccessKeyID != "AKIA123" || aws.SecretAccessKey != "secret123" || aws.Region != "us-east-1" {
		t.Fatalf("bad aws creds: %+v", aws)
	}
	ali := ts.Credentials.ForCloud("aliyun")
	if ali == nil || ali.AccessKeyID != "LTAI123" || ali.SecretAccessKey != "alisecret" {
		t.Fatalf("bad aliyun creds: %+v", ali)
	}
	if ts.Credentials.ForCloud("gcp") != nil {
		t.Fatal("unknown cloud should yield nil creds")
	}
}

func TestCredentialsValidation(t *testing.T) {
	if _, err := Parse([]byte("credentials:\n  aws:\n    access_key_id: x\nrun: hi\n")); err == nil {
		t.Fatal("missing aws secret should error")
	}
	if _, err := Parse([]byte("credentials:\n  aliyun:\n    access_key_secret: x\nrun: hi\n")); err == nil {
		t.Fatal("missing aliyun access key id should error")
	}
	if _, err := Parse([]byte("credentials:\n  aws:\n    access_key_id: a\n    secret_access_key: b\nrun: hi\n")); err != nil {
		t.Fatalf("complete aws creds should be valid: %v", err)
	}
}

func TestCredentialsGenericCloud(t *testing.T) {
	// A brand-new cloud (gcp) works with the generic map — no code change.
	ts, err := Parse([]byte(`
name: gcp
resources:
  cpus: 2+
credentials:
  gcp:
    access_key_id: GCP123
    secret_access_key: gcps
    region: us-central1
run: echo
`))
	if err != nil {
		t.Fatal(err)
	}
	g := ts.Credentials.ForCloud("gcp")
	if g == nil || g.AccessKeyID != "GCP123" || g.SecretAccessKey != "gcps" || g.Region != "us-central1" {
		t.Fatalf("bad gcp creds: %+v", g)
	}
}

func TestCredentialsLegacySecretField(t *testing.T) {
	// aliyun's legacy access_key_secret still maps to SecretAccessKey.
	ts, err := Parse([]byte(`
name: ali
credentials:
  aliyun:
    access_key_id: LTAI
    access_key_secret: als
run: echo
`))
	if err != nil {
		t.Fatal(err)
	}
	a := ts.Credentials.ForCloud("aliyun")
	if a == nil || a.SecretAccessKey != "als" {
		t.Fatalf("bad aliyun creds: %+v", a)
	}
}

func TestCredentialsJSONCamelAndOmitEmpty(t *testing.T) {
	ts, err := Parse([]byte(`
name: c
credentials:
  aws:
    access_key_id: AK
    secret_access_key: SK
run: echo
`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(ts)
	if err != nil {
		t.Fatal(err)
	}
	if !containsJSON(string(b), `"credentials":{"aws":{"accessKeyId":"AK"`) {
		t.Fatalf("credentials json wrong: %s", b)
	}
	// Empty credentials should be omitted.
	ts2 := &Task{Name: "n"}
	b2, _ := json.Marshal(ts2)
	if containsJSON(string(b2), "credentials") {
		t.Fatalf("empty credentials should be omitted: %s", b2)
	}
}

func containsJSON(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
