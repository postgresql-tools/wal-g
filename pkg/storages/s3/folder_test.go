// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package s3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lateos-ai/wal-g/pkg/storages/s3"
	"github.com/lateos-ai/wal-g/testtools"
)

func TestS3FolderValidate_S3ReturnsErr(t *testing.T) {
	config := &s3.Config{
		Bucket: "test",

		AccessKey: "AKIAIOSFODNN7EXAMPLE",

		SessionToken: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",

		Endpoint: "HTTP://s3.kek.lol.net/",

		Region: "region",
	}

	s3Client := testtools.NewMockS3Client(true, false)

	folder := s3.NewFolder(s3Client, nil, config.RootPath, config)

	err := folder.Validate()

	assert.Contains(t, err.Error(), "bad credentials")
}

func TestS3FolderValidate_S3DoesNotReturnErr(t *testing.T) {
	config := &s3.Config{
		Bucket: "test",

		AccessKey: "AKIAIOSFODNN7EXAMPLE",

		SessionToken: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",

		Endpoint: "HTTP://s3.kek.lol.net/",

		Region: "region",
	}

	s3Client := testtools.NewMockS3Client(false, false)

	folder := s3.NewFolder(s3Client, nil, config.RootPath, config)

	err := folder.Validate()

	assert.NoError(t, err)
}
