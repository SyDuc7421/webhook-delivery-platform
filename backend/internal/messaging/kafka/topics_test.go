package kafka

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestTopicsMatchCompose(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../../../deploy/docker-compose.yml")
	if err != nil {
		t.Fatalf("read compose: %v", err)
	}
	m := regexp.MustCompile(`for t in ([^;]+); do`).FindSubmatch(raw)
	if m == nil {
		t.Fatal("kafka-init topic loop not found in deploy/docker-compose.yml")
	}
	compose := strings.Fields(string(m[1]))
	code := AllTopics()
	slices.Sort(compose)
	slices.Sort(code)
	if !slices.Equal(compose, code) {
		t.Errorf("compose topics %v != code topics %v", compose, code)
	}
}

func TestName(t *testing.T) {
	t.Parallel()
	if got := Name("t42.", TopicEvents); got != "t42.events" {
		t.Errorf("Name = %q", got)
	}
}
