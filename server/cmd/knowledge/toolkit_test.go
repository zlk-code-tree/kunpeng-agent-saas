package main

import (
	"strings"
	"testing"
)

func TestToolkitSelectionExcludesRuntimeData(t *testing.T) {
	selected := []string{"README.md", "compose.sample.yml", "php/README.md", "php/8.2/compose.sample.yml", "php/8.2/sample.env", "php/8.2/Dockerfile", "readme/dify-local-deployment.md"}
	for _, path := range selected {
		if !toolkitFileAllowed(path) {
			t.Errorf("expected selected: %s", path)
		}
	}
	rejected := []string{"compose.yml", ".env", "mysql/8/.env", "php/8.2/compose.yml", "www/private/.env", "config/nginx/secret.conf", "php/8.2/config/php.ini"}
	for _, path := range rejected {
		if toolkitFileAllowed(path) {
			t.Errorf("expected rejected: %s", path)
		}
	}
}

func TestSanitizeToolkitTextMasksExampleCredentials(t *testing.T) {
	input := "MYSQL_ROOT_PASSWORD=root\nCF_Token=example\nCREATE USER 'x' IDENTIFIED BY 'admin';\nPHP_EXTENSIONS=redis,gd"
	clean := sanitizeToolkitText(input)
	for _, secret := range []string{"PASSWORD=root", "Token=example", "BY 'admin'"} {
		if strings.Contains(clean, secret) {
			t.Errorf("credential remained in indexed text: %s", secret)
		}
	}
	if !strings.Contains(clean, "PHP_EXTENSIONS=redis,gd") {
		t.Fatal("non-secret configuration was changed")
	}
}
