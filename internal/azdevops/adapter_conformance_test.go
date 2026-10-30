//go:build adapter

package azdevops_test

import (
	"github.com/hari-incoming/hazdo/internal/azdevops"
	"github.com/hari-incoming/hazdo/internal/provider"
)

// Compile-time assertion: Adapter must satisfy provider.Provider.
// This will fail to compile until task 5 creates the Adapter type.
var _ provider.Provider = (*azdevops.Adapter)(nil)
