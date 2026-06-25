package checkpointpolicy

import (
	"fmt"
	"strconv"
	"strings"
)

type CheckpointFamily string

const (
	CheckpointFamilyBranch CheckpointFamily = "branch"
	CheckpointFamilyRefs   CheckpointFamily = "refs"
)

type CheckpointFormat struct {
	Family CheckpointFamily
	Major  int
}

func ParseFormat(raw string) (CheckpointFormat, error) {
	familyRaw, majorRaw, ok := strings.Cut(raw, "-v")
	if !ok || familyRaw == "" || majorRaw == "" {
		return CheckpointFormat{}, fmt.Errorf("invalid checkpoint format %q", raw)
	}

	major, err := strconv.Atoi(majorRaw)
	if err != nil || major <= 0 {
		return CheckpointFormat{}, fmt.Errorf("invalid checkpoint major %q", majorRaw)
	}

	return CheckpointFormat{Family: CheckpointFamily(familyRaw), Major: major}, nil
}

func (f CheckpointFormat) String() string {
	if f.Family == "" || f.Major == 0 {
		return ""
	}
	return fmt.Sprintf("%s-v%d", f.Family, f.Major)
}

func CanRead(format CheckpointFormat) bool {
	return readFormats[format]
}

var branchV1Format = CheckpointFormat{Family: CheckpointFamilyBranch, Major: 1}

var readFormats = map[CheckpointFormat]bool{
	branchV1Format: true,
}
