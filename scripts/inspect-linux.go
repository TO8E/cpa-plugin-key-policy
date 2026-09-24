//go:build ignore

package main

import (
	"debug/elf"
	"fmt"
	"os"
	"sort"
)

func main() {
	if len(os.Args) != 2 {
		panic("usage: inspect-linux path.so")
	}
	f, err := elf.Open(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if f.Machine != elf.EM_X86_64 || f.Type != elf.ET_DYN || f.Class != elf.ELFCLASS64 {
		panic("expected a Linux x86_64 shared library")
	}
	symbols, err := f.DynamicSymbols()
	if err != nil {
		panic(err)
	}
	entry := false
	for _, symbol := range symbols {
		if symbol.Name == "cliproxy_plugin_init" && symbol.Section != elf.SHN_UNDEF {
			entry = true
		}
	}
	if !entry {
		panic("missing plugin ABI entry point")
	}
	libraries, err := f.ImportedLibraries()
	if err != nil {
		panic(err)
	}
	imports, err := f.ImportedSymbols()
	if err != nil {
		panic(err)
	}
	versions := make(map[string]bool)
	for _, symbol := range imports {
		if symbol.Version != "" {
			versions[symbol.Version] = true
		}
	}
	names := make([]string, 0, len(versions))
	for version := range versions {
		names = append(names, version)
	}
	sort.Strings(names)
	fmt.Printf("ELF: %s %s %s\nABI: cliproxy_plugin_init exported\nDynamic dependencies: %v\nSymbol versions: %v\n", f.Class, f.Machine, f.Type, libraries, names)
}
