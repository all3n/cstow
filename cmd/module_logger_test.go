package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModuleLoggerTail(t *testing.T) {
	var log moduleLogger
	log.module = "testmod"
	log.Write([]byte("line1\nline2\nline3\nline4\nline5\n"))

	lines := log.tailLines(3)
	assert.Equal(t, []string{"line3", "line4", "line5"}, lines)
}

func TestModuleLoggerTailFewerThanMax(t *testing.T) {
	var log moduleLogger
	log.module = "testmod"
	log.Write([]byte("line1\nline2\n"))

	lines := log.tailLines(20)
	assert.Equal(t, []string{"line1", "line2"}, lines)
}

func TestModuleLoggerTailEmpty(t *testing.T) {
	var log moduleLogger
	lines := log.tailLines(20)
	assert.Equal(t, 0, len(lines))
}
