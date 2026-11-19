package revisions

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestRevisions(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Revisions Suite")
}
