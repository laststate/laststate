// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

// Package symbolserver downloads firmware from an HTTP symbol service by build id.
package symbolserver

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/laststate/relay/internal/artifact"
)

// Client fetches firmware artifacts by build id from an HTTP symbol server.
type Client struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

// FetchBuildID downloads an artifact when missing from the local catalog.
func (c *Client) FetchBuildID(ctx context.Context, artifactDir, buildID string) (artifact.Artifact, error) {
	buildID = strings.TrimSpace(buildID)
	if buildID == "" {
		return artifact.Artifact{}, fmt.Errorf("empty build id")
	}
	if existing, err := artifact.FindByBuildIDFrom(artifactDir, buildID); err == nil {
		return existing, nil
	}
	if c.BaseURL == "" {
		return artifact.Artifact{}, fmt.Errorf("symbol server not configured")
	}
	if c.Client == nil {
		c.Client = &http.Client{Timeout: 60 * time.Second}
	}
	endpoint, err := url.JoinPath(c.BaseURL, "v1", "symbols", buildID)
	if err != nil {
		return artifact.Artifact{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return artifact.Artifact{}, err
	}
	if c.Token != "" {
		request.Header.Set("Authorization", "Bearer "+c.Token)
	}
	response, err := c.Client.Do(request)
	if err != nil {
		return artifact.Artifact{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return artifact.Artifact{}, fmt.Errorf("symbol server returned HTTP %d", response.StatusCode)
	}
	temporary, err := os.CreateTemp(artifactDir, "symbol-*.elf")
	if err != nil {
		return artifact.Artifact{}, err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err := io.Copy(temporary, io.LimitReader(response.Body, 256<<20)); err != nil {
		temporary.Close()
		return artifact.Artifact{}, err
	}
	if err := temporary.Close(); err != nil {
		return artifact.Artifact{}, err
	}
	item, err := artifact.AddTo(artifactDir, name)
	if err != nil {
		return artifact.Artifact{}, err
	}
	_ = filepath.Dir(item.Path)
	return item, nil
}
