//go:build tools
// +build tools

package tools

// Tool and library dependencies that are required by the project
// but not directly imported in the main package yet.
// These will be imported by internal packages as they are implemented.
import (
	_ "github.com/redis/go-redis/v9"
	_ "gorm.io/driver/postgres"
	_ "gorm.io/gorm"
	_ "pgregory.net/rapid"
)
