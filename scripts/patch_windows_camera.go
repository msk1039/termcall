package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	// Find the go env GOMODCACHE
	cmd := exec.Command("go", "env", "GOMODCACHE")
	out, err := cmd.Output()
	if err != nil {
		fmt.Printf("Failed to get GOMODCACHE: %v\n", err)
		os.Exit(1)
	}

	modCache := strings.TrimSpace(string(out))
	targetDir := filepath.Join(modCache, "github.com", "pion", "mediadevices@v0.10.0", "pkg", "driver", "camera")

	hppPath := filepath.Join(targetDir, "camera_windows.hpp")
	cppPath := filepath.Join(targetDir, "camera_windows.cpp")

	if _, err := os.Stat(hppPath); os.IsNotExist(err) {
		fmt.Printf("Could not find %s\nPlease ensure pion/mediadevices@v0.10.0 is downloaded.\n", hppPath)
		os.Exit(1)
	}

	patchFile(hppPath, []string{
		"HRESULT SampleCB", "HRESULT STDMETHODCALLTYPE SampleCB",
		"HRESULT BufferCB", "HRESULT STDMETHODCALLTYPE BufferCB",
	})

	patchFile(cppPath, []string{
		"HRESULT SampleGrabberCallback::SampleCB", "HRESULT STDMETHODCALLTYPE SampleGrabberCallback::SampleCB",
		"HRESULT SampleGrabberCallback::BufferCB", "HRESULT STDMETHODCALLTYPE SampleGrabberCallback::BufferCB",
	})

	fmt.Println("Successfully patched pion/mediadevices for Windows MSYS2 GCC compilation!")
}

func patchFile(path string, replacements []string) {
	// Read file
	content, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("Failed to read %s: %v\n", path, err)
		os.Exit(1)
	}

	// Change permissions to allow writing
	err = os.Chmod(path, 0644)
	if err != nil {
		fmt.Printf("Warning: failed to chmod %s: %v\n", path, err)
	}

	text := string(content)
	modified := false

	for i := 0; i < len(replacements); i += 2 {
		old := replacements[i]
		newStr := replacements[i+1]
		if strings.Contains(text, old) && !strings.Contains(text, newStr) {
			text = strings.Replace(text, old, newStr, -1)
			modified = true
		}
	}

	if modified {
		err = os.WriteFile(path, []byte(text), 0644)
		if err != nil {
			fmt.Printf("Failed to write %s: %v\n", path, err)
			os.Exit(1)
		}
		fmt.Printf("Patched: %s\n", path)
	} else {
		fmt.Printf("Already patched: %s\n", path)
	}
}
