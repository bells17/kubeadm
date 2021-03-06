/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package base implements functionality to build the kind base image
package base

import (
	"go/build"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"k8s.io/kubeadm/kinder/pkg/exec"
	"sigs.k8s.io/kind/pkg/fs"
	"sigs.k8s.io/kind/pkg/util"
)

// DefaultImage is the default name:tag of the built base image
const DefaultImage = "kindest/base:latest"

// BuildContext is used to build the kind node base image, and contains
// build configuration
type BuildContext struct {
	// option fields
	sourceDir string
	image     string
	// non option fields
	goCmd string // TODO: should be an option possibly
	arch  string // TODO: should be an option
}

// Option is BuildContext configuration option supplied to NewBuildContext
type Option func(*BuildContext)

// WithSourceDir configures a NewBuildContext to use the source dir `sourceDir`
func WithSourceDir(sourceDir string) Option {
	return func(b *BuildContext) {
		b.sourceDir = sourceDir
	}
}

// WithImage configures a NewBuildContext to tag the built image with `name`
func WithImage(image string) Option {
	return func(b *BuildContext) {
		b.image = image
	}
}

// NewBuildContext creates a new BuildContext with
// default configuration
func NewBuildContext(options ...Option) *BuildContext {
	ctx := &BuildContext{
		image: DefaultImage,
		goCmd: "go",
		arch:  util.GetArch(),
	}
	for _, option := range options {
		option(ctx)
	}
	return ctx
}

// Build builds the cluster node image, the sourcedir must be set on
// the NodeImageBuildContext
func (c *BuildContext) Build() (err error) {
	// create tempdir to build in
	tmpDir, err := fs.TempDir("", "kind-base-image")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	// populate with image sources
	// if SourceDir is unset then try to autodetect source dir
	buildDir := tmpDir
	if c.sourceDir == "" {
		pkg, err := build.Default.Import("k8s.io/kubeadm/kinder", build.Default.GOPATH, build.FindOnly)
		if err != nil {
			return errors.Wrap(err, "failed to locate sources")
		}
		c.sourceDir = filepath.Join(pkg.Dir, "images", "base", "docker")
	}

	err = fs.Copy(c.sourceDir, buildDir)
	if err != nil {
		log.Errorf("failed to copy sources to build dir %v", err)
		return err
	}

	log.Infof("Building base image in: %s", buildDir)

	// build the entrypoint binary first
	if err := c.buildEntrypoint(buildDir); err != nil {
		return err
	}

	// then the actual docker image
	return c.buildImage(buildDir)
}

// builds the entrypoint binary
func (c *BuildContext) buildEntrypoint(dir string) error {
	// NOTE: this binary only uses the go1 stdlib, and is a single file
	entrypointSrc := filepath.Join(dir, "entrypoint", "main.go")
	entrypointDest := filepath.Join(dir, "entrypoint", "entrypoint")

	cmd := exec.NewHostCmd(c.goCmd, "build", "-o", entrypointDest, entrypointSrc)
	cmd.SetEnv(append(os.Environ(), "GOOS=linux", "GOARCH="+c.arch)...)

	// actually build
	log.Info("Building entrypoint binary ...")
	if err := cmd.RunWithEcho(); err != nil {
		log.Errorf("Entrypoint build Failed! %v", err)
		return err
	}
	log.Info("Entrypoint build completed.")
	return nil
}

func (c *BuildContext) buildImage(dir string) error {
	// build the image, tagged as tagImageAs, using the our tempdir as the context
	cmd := exec.NewHostCmd("docker", "build", "-t", c.image, dir)
	log.Info("Starting Docker build ...")

	if err := cmd.RunWithEcho(); err != nil {
		log.Errorf("Docker build Failed! %v", err)
		return err
	}
	log.Info("Docker build completed.")
	return nil
}
