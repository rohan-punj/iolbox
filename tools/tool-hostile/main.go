// tool-hostile is an intentionally small adversarial child for the P0 spike.
// It reports attempts, not just exit status, so the acceptance harness can
// distinguish the expected accepted filesystem boundary from isolation
// failures. It is not a production sandbox or a security test framework.
package main

import (
	"fmt"
	"os"
	"time"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--grandchild":
			for {
				time.Sleep(time.Second)
			}
		case "--cap-regain-child":
			if err := attemptCapRegain(); err == nil {
				fmt.Println("CAP_REGAIN_DENIED")
				return
			}
			fmt.Println("CAP_REGAIN_SUCCEEDED")
			os.Exit(1)
		case "--orphan", "--sleep-child", "--linger":
			for {
				time.Sleep(time.Second)
			}
		case "--memory-hog":
			memoryHog()
		case "--fork-bomb":
			forkBomb()
		}
	}

	if err := runHostileProbe(); err != nil {
		fmt.Printf("P0_HOSTILE_FAIL %v\n", err)
		os.Exit(1)
	}

	if orphanPath := os.Getenv("IOLBOX_HOSTILE_ORPHAN_PID_FILE"); orphanPath != "" {
		if err := startOrphan(orphanPath); err != nil {
			fmt.Printf("ORPHAN_FAIL %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Println("P0_HOSTILE_PASS")
	for os.Getenv("IOLBOX_HOSTILE_LINGER") == "1" {
		time.Sleep(time.Second)
	}
}

func startOrphan(pidPath string) error {
	child, err := startSelf("--orphan")
	if err != nil {
		return err
	}
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", child)), 0o644); err != nil {
		return err
	}
	fmt.Printf("ORPHAN_PID %d\n", child)
	return nil
}

func startSelf(arg string) (int, error) {
	child, err := os.StartProcess("/proc/self/exe", []string{"tool-hostile", arg}, &os.ProcAttr{
		Env:   os.Environ(),
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
	})
	if err != nil {
		return 0, err
	}
	return child.Pid, nil
}
