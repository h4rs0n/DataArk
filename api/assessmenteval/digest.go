package assessmenteval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

func ManifestDigest(manifest Manifest) (string, error) {
	manifest.Digest = ""
	payload, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func ValidateArtifactLink(manifest Manifest, digest string) error {
	if digest == "" || digest != manifest.Digest {
		return fmt.Errorf("artifact manifest digest %q does not match %q", digest, manifest.Digest)
	}
	return nil
}
