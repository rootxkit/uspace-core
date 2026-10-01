// Command manifest-list prints "<file> <package>" per line from the vectors
// manifest, for CI shell steps.
package main

import (
	"fmt"

	"github.com/rootxkit/uspace-core/vectors"
)

func main() {
	for _, e := range vectors.Manifest {
		fmt.Printf("%s %s\n", e.File, e.Package)
	}
}
