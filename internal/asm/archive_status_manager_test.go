// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package asm_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lateos-ai/wal-g/internal/asm"
)

type pair struct {
	testData string

	expectedResult string
}

func TestGetOnlyWalName(t *testing.T) {
	samples := make([]pair, 0, 0)

	samples = append(samples, pair{"123456765.done", "123456765"})

	samples = append(samples, pair{"123456765.history.ready", "123456765.history"})

	samples = append(samples, pair{"somedir/123456765.done", "123456765"})

	for _, sample := range samples {
		assert.Equal(t, sample.expectedResult, asm.GetOnlyWalName(sample.testData))
	}
}
