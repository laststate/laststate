package config

import (
	"os"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	// Clear relevant env vars
	envVars := []string{
		"TRACE_LISTEN", "TRACE_PUBLIC_URL", "TRACE_DATABASE_URL",
		"TRACE_MOCK", "CHAOS_ENABLED", "PUBLIC_CRASH_API",
		"SOUNDBOARD_ENABLED", "MEMORIAL_ENABLED", "ANOMALY_ENABLED",
	}
	for _, v := range envVars {
		os.Unsetenv(v)
	}

	c := Load()

	if c.Listen != ":8080" {
		t.Errorf("expected listen :8080, got %s", c.Listen)
	}
	if c.Mode != "all" {
		t.Errorf("expected mode all, got %s", c.Mode)
	}
	if c.WorkerN != 2 {
		t.Errorf("expected 2 workers, got %d", c.WorkerN)
	}
	if c.RetentionDays != 90 {
		t.Errorf("expected 90 retention days, got %d", c.RetentionDays)
	}
}

func TestLoad_MockMode(t *testing.T) {
	os.Setenv("TRACE_MOCK", "true")
	defer os.Unsetenv("TRACE_MOCK")

	c := Load()
	if !c.MockMode {
		t.Error("expected MockMode to be true")
	}
}

func TestLoad_ChaosEnabled(t *testing.T) {
	os.Setenv("CHAOS_ENABLED", "true")
	defer os.Unsetenv("CHAOS_ENABLED")

	c := Load()
	if !c.ChaosEnabled {
		t.Error("expected ChaosEnabled to be true")
	}
	if c.ChaosAdapter != "serial" {
		t.Errorf("expected chaos adapter serial, got %s", c.ChaosAdapter)
	}
}

func TestLoad_PublicCrashAPI(t *testing.T) {
	os.Setenv("PUBLIC_CRASH_API", "false")
	defer os.Unsetenv("PUBLIC_CRASH_API")

	c := Load()
	if c.PublicCrashAPI {
		t.Error("expected PublicCrashAPI to be false")
	}
}

func TestLoad_Soundboard(t *testing.T) {
	os.Setenv("SOUNDBOARD_ENABLED", "true")
	os.Setenv("SOUNDBOARD_VOLUME", "75")
	defer os.Unsetenv("SOUNDBOARD_ENABLED")
	defer os.Unsetenv("SOUNDBOARD_VOLUME")

	c := Load()
	if !c.SoundboardEnabled {
		t.Error("expected SoundboardEnabled to be true")
	}
	if c.SoundboardVolume != 0.75 {
		t.Errorf("expected volume 0.75, got %f", c.SoundboardVolume)
	}
}

func TestLoad_GitHubConfig(t *testing.T) {
	os.Setenv("TRACE_GITHUB_TOKEN", "ghp_test123")
	os.Setenv("TRACE_GITHUB_REPO", "org/repo")
	os.Setenv("TRACE_GITHUB_DEFAULT_BRANCH", "develop")
	defer os.Unsetenv("TRACE_GITHUB_TOKEN")
	defer os.Unsetenv("TRACE_GITHUB_REPO")
	defer os.Unsetenv("TRACE_GITHUB_DEFAULT_BRANCH")

	c := Load()
	if c.GitHubToken != "ghp_test123" {
		t.Errorf("expected GitHub token, got %s", c.GitHubToken)
	}
	if c.GitHubRepo != "org/repo" {
		t.Errorf("expected GitHub repo, got %s", c.GitHubRepo)
	}
	if c.GitHubBranch != "develop" {
		t.Errorf("expected GitHub branch develop, got %s", c.GitHubBranch)
	}
}

func TestLoad_FleetHealthInterval(t *testing.T) {
	os.Setenv("FLEET_HEALTH_RECALC_INTERVAL_MIN", "30")
	defer os.Unsetenv("FLEET_HEALTH_RECALC_INTERVAL_MIN")

	c := Load()
	if c.FleetHealthRecalcInterval != 30 {
		t.Errorf("expected recalc interval 30, got %d", c.FleetHealthRecalcInterval)
	}
}

func TestLoad_PostmortemConfig(t *testing.T) {
	os.Setenv("TRACE_POSTMORTEM_API_KEY", "sk-test")
	os.Setenv("TRACE_POSTMORTEM_MAX_FREE", "50")
	defer os.Unsetenv("TRACE_POSTMORTEM_API_KEY")
	defer os.Unsetenv("TRACE_POSTMORTEM_MAX_FREE")

	c := Load()
	if c.PostmortemAPIKey != "sk-test" {
		t.Errorf("expected API key, got %s", c.PostmortemAPIKey)
	}
	if c.PostmortemMaxFree != 50 {
		t.Errorf("expected max free 50, got %d", c.PostmortemMaxFree)
	}
}

func TestLoad_DNAThreshold(t *testing.T) {
	os.Setenv("DNA_DETECTION_THRESHOLD", "90")
	defer os.Unsetenv("DNA_DETECTION_THRESHOLD")

	c := Load()
	if c.DNADetectionThreshold != 0.9 {
		t.Errorf("expected threshold 0.9, got %f", c.DNADetectionThreshold)
	}
}

func TestLoad_ModeCorrection(t *testing.T) {
	os.Setenv("TRACE_MODE", "invalid")
	defer os.Unsetenv("TRACE_MODE")

	c := Load()
	if c.Mode != "all" {
		t.Errorf("expected mode corrected to all, got %s", c.Mode)
	}
	if !c.modeWasCorrected {
		t.Error("expected modeWasCorrected to be true")
	}
}

func TestLoad_DeploymentDefaultEnterprise(t *testing.T) {
	os.Unsetenv("TRACE_DEPLOYMENT")
	c := Load()
	if c.Deployment != "enterprise" {
		t.Errorf("expected default deployment enterprise, got %s", c.Deployment)
	}
	if !c.IsEnterprise() {
		t.Error("expected IsEnterprise() true by default")
	}
	if c.IsLocal() {
		t.Error("expected IsLocal() false by default")
	}
}

func TestLoad_DeploymentLocalUnlocks(t *testing.T) {
	os.Setenv("TRACE_DEPLOYMENT", "local")
	defer os.Unsetenv("TRACE_DEPLOYMENT")

	c := Load()
	if c.Deployment != "local" {
		t.Errorf("expected deployment local, got %s", c.Deployment)
	}
	if !c.IsLocal() {
		t.Error("expected IsLocal() true")
	}
	if c.IsEnterprise() {
		t.Error("expected IsEnterprise() false")
	}
	if !c.OpenUI {
		t.Error("expected OpenUI forced true in local mode")
	}
	if !c.AllowPublicRegister {
		t.Error("expected AllowPublicRegister forced true in local mode")
	}
}

func TestLoad_DeploymentCorrection(t *testing.T) {
	os.Setenv("TRACE_DEPLOYMENT", "bogus")
	defer os.Unsetenv("TRACE_DEPLOYMENT")

	c := Load()
	if c.Deployment != "enterprise" {
		t.Errorf("expected deployment corrected to enterprise, got %s", c.Deployment)
	}
}

func TestValidateProduction_LocalModeAllowed(t *testing.T) {
	os.Setenv("TRACE_ENV", "production")
	defer os.Unsetenv("TRACE_ENV")

	c := Config{
		Deployment:          "local",
		OpenUI:              true,
		AllowPublicRegister: true,
		AdminPassword:       "secure_password",
		CookieSecure:        true,
	}

	warnings, err := c.ValidateProduction()
	if err != nil {
		t.Fatalf("expected local deployment to be allowed in production, got error: %v", err)
	}
	if len(warnings) > 0 {
		t.Errorf("expected no warnings for local mode, got %d", len(warnings))
	}
}

func TestLoad_SMTPDefaults(t *testing.T) {
	c := Load()
	if c.SMTPPort != 587 {
		t.Errorf("expected SMTP port 587, got %d", c.SMTPPort)
	}
	if c.SMTPSecure != "" {
		t.Errorf("expected empty SMTP secure, got %s", c.SMTPSecure)
	}
}

func TestLoad_SMTPSecureTLS(t *testing.T) {
	os.Setenv("TRACE_SMTP_HOST", "smtp.example.com")
	defer os.Unsetenv("TRACE_SMTP_HOST")

	c := Load()
	if c.SMTPSecure != "tls" {
		t.Errorf("expected SMTP secure tls, got %s", c.SMTPSecure)
	}
}

func TestValidateProduction_Valid(t *testing.T) {
	os.Setenv("TRACE_ENV", "production")
	defer os.Unsetenv("TRACE_ENV")

	c := Config{
		OpenUI:        false,
		AdminPassword: "secure_password",
		CookieSecure:  true,
	}

	warnings, err := c.ValidateProduction()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) > 0 {
		t.Errorf("expected no warnings, got %d", len(warnings))
	}
}

func TestValidateProduction_InsecureAdminPassword(t *testing.T) {
	os.Setenv("TRACE_ENV", "production")
	defer os.Unsetenv("TRACE_ENV")

	c := Config{
		AdminPassword: "admin",
		CookieSecure:  true,
	}

	_, err := c.ValidateProduction()
	if err == nil {
		t.Error("expected error for insecure admin password")
	}
}

func TestValidateProduction_OpenUI(t *testing.T) {
	os.Setenv("TRACE_ENV", "production")
	defer os.Unsetenv("TRACE_ENV")

	c := Config{
		OpenUI:       true,
		CookieSecure: true,
	}

	_, err := c.ValidateProduction()
	if err == nil {
		t.Error("expected error for OpenUI in production")
	}
}

func TestValidateProduction_PublicRegister(t *testing.T) {
	os.Setenv("TRACE_ENV", "production")
	defer os.Unsetenv("TRACE_ENV")

	c := Config{
		AllowPublicRegister: true,
		CookieSecure:        true,
	}

	_, err := c.ValidateProduction()
	if err == nil {
		t.Error("expected error for public register in production")
	}
}

func TestValidateProduction_OIDCAutoJoin(t *testing.T) {
	os.Setenv("TRACE_ENV", "production")
	defer os.Unsetenv("TRACE_ENV")

	c := Config{
		OIDCAutoJoin: true,
		CookieSecure: true,
	}

	_, err := c.ValidateProduction()
	if err == nil {
		t.Error("expected error for OIDC auto join in production")
	}
}

func TestValidateProduction_SAMLInsecure(t *testing.T) {
	os.Setenv("TRACE_ENV", "production")
	defer os.Unsetenv("TRACE_ENV")

	c := Config{
		SAMLInsecure: true,
		CookieSecure: true,
	}

	_, err := c.ValidateProduction()
	if err == nil {
		t.Error("expected error for SAML insecure in production")
	}
}

func TestDecodeHex(t *testing.T) {
	tests := []struct {
		input string
		want  []byte
	}{
		{"48656c6c6f", []byte("Hello")},
		{"00ff", []byte{0x00, 0xff}},
		{"", nil},
	}

	for _, tt := range tests {
		got, err := decodeHex(tt.input)
		if err != nil {
			t.Errorf("decodeHex(%q): error %v", tt.input, err)
			continue
		}
		if len(got) != len(tt.want) {
			t.Errorf("decodeHex(%q): got %d bytes, want %d", tt.input, len(got), len(tt.want))
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("decodeHex(%q): byte %d: got %02x, want %02x", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestDecodeBase64(t *testing.T) {
	got, err := decodeBase64("SGVsbG8=")
	if err != nil {
		t.Fatalf("decodeBase64: error %v", err)
	}
	if string(got) != "Hello" {
		t.Errorf("decodeBase64: got %q, want %q", string(got), "Hello")
	}
}

func TestSecretsKeyValidation(t *testing.T) {
	os.Setenv("TRACE_ENV", "production")
	defer os.Unsetenv("TRACE_ENV")

	// 32-byte hex key (64 hex chars = 32 bytes)
	c := Config{
		SecretsKey:   "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		CookieSecure: true,
	}

	warnings, err := c.ValidateProduction()
	if err != nil {
		t.Fatalf("expected no error for valid 32-byte hex key, got: %v", err)
	}
	if len(warnings) > 0 {
		t.Errorf("expected no warnings, got %d", len(warnings))
	}

	// Too short key
	c.SecretsKey = "0123456789abcdef"
	_, err = c.ValidateProduction()
	if err == nil {
		t.Error("expected error for too-short secrets key")
	}
}
