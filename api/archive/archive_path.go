package archive

import (
	"DataArk/config"
	"fmt"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type ArchiveDocumentPath struct {
	RequestPath string
	Domain      string
	Filename    string
	AbsPath     string
}

func ResolveArchiveDocumentPath(rawPath string) (*ArchiveDocumentPath, error) {
	requestPath := strings.TrimSpace(rawPath)
	if requestPath == "" {
		return nil, fmt.Errorf("empty archive path")
	}

	if parsedURL, err := neturl.Parse(requestPath); err == nil && parsedURL.Path != "" {
		requestPath = parsedURL.Path
	}

	requestPath = strings.TrimPrefix(requestPath, "/")
	const archivePrefix = "archive/"
	if !strings.HasPrefix(requestPath, archivePrefix) {
		return nil, fmt.Errorf("archive path must start with /archive/")
	}

	archiveRelPath, err := neturl.PathUnescape(strings.TrimPrefix(requestPath, archivePrefix))
	if err != nil {
		return nil, fmt.Errorf("invalid archive path escape: %w", err)
	}

	segments := strings.Split(archiveRelPath, "/")
	if len(segments) < 2 {
		return nil, fmt.Errorf("archive path must include domain and file name")
	}
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return nil, fmt.Errorf("unsafe archive path segment")
		}
	}

	cleanArchiveRelPath := path.Clean(strings.Join(segments, "/"))
	rootAbs, err := filepath.Abs(config.ARCHIVEFILELOACTION)
	if err != nil {
		return nil, err
	}
	targetAbs, err := filepath.Abs(filepath.Join(rootAbs, filepath.FromSlash(cleanArchiveRelPath)))
	if err != nil {
		return nil, err
	}
	relToRoot, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return nil, err
	}
	if relToRoot == "." || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(os.PathSeparator)) || filepath.IsAbs(relToRoot) {
		return nil, fmt.Errorf("archive path escapes archive root")
	}

	return &ArchiveDocumentPath{
		RequestPath: "/" + path.Join("archive", cleanArchiveRelPath),
		Domain:      strings.TrimSpace(segments[0]),
		Filename:    strings.Join(segments[1:], "/"),
		AbsPath:     targetAbs,
	}, nil
}
