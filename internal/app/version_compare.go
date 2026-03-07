package app

import (
	"strconv"
	"strings"
)

// VersionMismatchKind 表示版本一致性檢查結果的類型。
type VersionMismatchKind int

const (
	// VersionMatch 版本一致，無需額外處理。
	VersionMatch VersionMismatchKind = iota
	// VersionMismatchCurrentNewer 目前執行檔版本高於服務安裝版本（可升級）。
	VersionMismatchCurrentNewer
	// VersionMismatchCurrentOlder 目前執行檔版本低於服務安裝版本（降版受阻）。
	VersionMismatchCurrentOlder
)

// VersionCheckResult 封裝版本一致性檢查的完整結果，供呼叫端依類型決策。
type VersionCheckResult struct {
	Kind      VersionMismatchKind
	Current   string // 目前執行檔版本
	Installed string // 服務安裝版本
	RootDir   string // 從設定讀取，供升級流程傳入 initAndExit
}

// compareVersions 比較兩個版本字串，回傳：
//
//	-1: a 低於 b
//	 0: a 等於 b
//	 1: a 高於 b
//
// 支援 "v1.4"、"v1.4-48-g519be99"（git describe）格式。
// 比較順序：major → minor → patch → git-commits 數；
// git hash 部份不影響比較（兩個 hash 相異但 commits 相同視為相等）。
func compareVersions(a, b string) int {
	pa := parseVersionParts(strings.TrimSpace(a))
	pb := parseVersionParts(strings.TrimSpace(b))

	// 依序比較 major、minor、patch 等數字段
	maxLen := len(pa.nums)
	if len(pb.nums) > maxLen {
		maxLen = len(pb.nums)
	}
	for i := 0; i < maxLen; i++ {
		var na, nb int
		if i < len(pa.nums) {
			na = pa.nums[i]
		}
		if i < len(pb.nums) {
			nb = pb.nums[i]
		}
		if na < nb {
			return -1
		}
		if na > nb {
			return 1
		}
	}

	// semver 相同，再比較 git commits 數（越大越新）
	if pa.commits < pb.commits {
		return -1
	}
	if pa.commits > pb.commits {
		return 1
	}
	return 0
}

// versionParts 儲存解析後的版本數字段與 git commits 數。
type versionParts struct {
	nums    []int
	commits int
}

// parseVersionParts 解析版本字串。
//
// 支援格式：
//   - "v1.4"
//   - "v1.4-48-g519be99"（git describe）
//
// 範例："v1.4-48-g519be99" → nums=[1,4], commits=48
func parseVersionParts(v string) versionParts {
	s := strings.TrimPrefix(strings.TrimSpace(v), "v")

	// 分割最多 3 段：semver、commits、hash
	parts := strings.SplitN(s, "-", 3)
	semverPart := parts[0]

	var commits int
	if len(parts) >= 2 {
		if n, err := strconv.Atoi(parts[1]); err == nil {
			commits = n
		}
	}

	numStrs := strings.Split(semverPart, ".")
	nums := make([]int, 0, len(numStrs))
	for _, ns := range numStrs {
		if n, err := strconv.Atoi(ns); err == nil {
			nums = append(nums, n)
		}
	}

	return versionParts{nums: nums, commits: commits}
}
