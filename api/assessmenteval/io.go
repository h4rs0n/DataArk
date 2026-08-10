package assessmenteval

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func ReadManifest(path string) (Manifest, error) {
	var manifest Manifest
	if err := readJSON(path, &manifest); err != nil {
		return manifest, err
	}
	if manifest.Version != ManifestVersion || manifest.Digest == "" {
		return manifest, errors.New("unsupported or incomplete article assessment manifest")
	}
	digest, err := ManifestDigest(manifest)
	if err != nil {
		return manifest, err
	}
	if digest != manifest.Digest {
		return manifest, errors.New("article assessment manifest digest mismatch")
	}
	return manifest, nil
}

func ReadLabels(path string) (LabelSet, error) {
	var labels LabelSet
	err := readJSON(path, &labels)
	if err == nil && labels.Version != LabelVersion {
		err = errors.New("unsupported label file version")
	}
	return labels, err
}

func ReadScores(path string) (ScoreSet, error) {
	var scores ScoreSet
	err := readJSON(path, &scores)
	if err == nil && scores.Version != ScoreVersion {
		err = errors.New("unsupported score file version")
	}
	return scores, err
}

func WritePrivateJSON(path string, value interface{}) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	return WritePrivateFile(path, payload)
}

func WritePrivateFile(path string, payload []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".article-assessment-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(payload); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

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

func readJSON(path string, output interface{}) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	return nil
}
