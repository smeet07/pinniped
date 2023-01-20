// Copyright 2022-2023 the Pinniped contributors. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/Masterminds/semver/v3"
)

var (
	go1195 = semver.MustParse("1.19.5")
)

func X509UntrustedCertError(commonName string) string {
	// https://github.com/golang/go/issues/57427
	// Golang 1.19.5 no longer returns a different error for darwin
	runtimeVersion, err := semver.NewVersion(strings.ReplaceAll("foo"+runtime.Version(), "go", ""))

	if err != nil || runtimeVersion == nil {
		return fmt.Sprintf("Runtime version %s should match format go.1.19.5", runtime.Version())
	}

	if runtime.GOOS == "darwin" && runtimeVersion.LessThan(go1195) {
		// Golang use's macos' x509 verification APIs on darwin.
		// This output slightly different error messages than golang's
		// own x509 verification.
		return fmt.Sprintf(`x509: “%s” certificate is not trusted`, commonName)
	}
	return `x509: certificate signed by unknown authority`
}
