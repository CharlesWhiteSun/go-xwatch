package app

import "testing"

// ── compareVersions 單元測試 ──────────────────────────────────────────────

// TestCompareVersions_Equal 確認相同版本回傳 0。
func TestCompareVersions_Equal(t *testing.T) {
	if got := compareVersions("v1.4", "v1.4"); got != 0 {
		t.Errorf("相同版本應回傳 0，實際：%d", got)
	}
}

// TestCompareVersions_GitDescribeEqualCommits 相同 semver 與 commits、不同 hash 視為相等。
func TestCompareVersions_GitDescribeEqualCommits(t *testing.T) {
	if got := compareVersions("v1.4-48-g519be99", "v1.4-48-gabcdef0"); got != 0 {
		t.Errorf("相同 semver 與 commits 應視為相等（忽略 hash），實際：%d", got)
	}
}

// TestCompareVersions_CurrentNewerBySemver v1.5 > v1.4 。
func TestCompareVersions_CurrentNewerBySemver(t *testing.T) {
	if got := compareVersions("v1.5", "v1.4"); got != 1 {
		t.Errorf("v1.5 > v1.4 應回傳 1，實際：%d", got)
	}
}

// TestCompareVersions_CurrentOlderBySemver v1.4 < v1.5 。
func TestCompareVersions_CurrentOlderBySemver(t *testing.T) {
	if got := compareVersions("v1.4", "v1.5"); got != -1 {
		t.Errorf("v1.4 < v1.5 應回傳 -1，實際：%d", got)
	}
}

// TestCompareVersions_CurrentNewerByCommits commits 48 > 46 。
func TestCompareVersions_CurrentNewerByCommits(t *testing.T) {
	if got := compareVersions("v1.4-48-g519be99", "v1.4-46-gd57936b"); got != 1 {
		t.Errorf("commits 48 > 46 應回傳 1，實際：%d", got)
	}
}

// TestCompareVersions_CurrentOlderByCommits commits 46 < 48 。
func TestCompareVersions_CurrentOlderByCommits(t *testing.T) {
	if got := compareVersions("v1.4-46-gd57936b", "v1.4-48-g519be99"); got != -1 {
		t.Errorf("commits 46 < 48 應回傳 -1，實際：%d", got)
	}
}

// TestCompareVersions_GitDescribeVsExactTag 帶commits 的 git describe 應比精確 tag 新。
func TestCompareVersions_GitDescribeVsExactTag(t *testing.T) {
	if got := compareVersions("v1.4-1-gabcdef0", "v1.4"); got != 1 {
		t.Errorf("v1.4-1-g... 應比 v1.4 tag 新（commits>0），實際：%d", got)
	}
}

// TestCompareVersions_ExactTagVsGitDescribe 精確 tag 應比帶 commits 的 describe 舊。
func TestCompareVersions_ExactTagVsGitDescribe(t *testing.T) {
	if got := compareVersions("v1.4", "v1.4-1-gabcdef0"); got != -1 {
		t.Errorf("v1.4 tag 應比 v1.4-1-g... 舊，實際：%d", got)
	}
}

// TestCompareVersions_MajorVersionDifference v2.0 > v1.9 。
func TestCompareVersions_MajorVersionDifference(t *testing.T) {
	if got := compareVersions("v2.0", "v1.9"); got != 1 {
		t.Errorf("v2.0 > v1.9 應回傳 1，實際：%d", got)
	}
}

// TestCompareVersions_Whitespace 含空白的相同版本視為相等。
func TestCompareVersions_Whitespace(t *testing.T) {
	if got := compareVersions(" v1.4 ", " v1.4 "); got != 0 {
		t.Errorf("含空白的相同版本應視為相等，實際：%d", got)
	}
}

// TestCompareVersions_PatchVersion v1.4.1 > v1.4.0 。
func TestCompareVersions_PatchVersion(t *testing.T) {
	if got := compareVersions("v1.4.1", "v1.4.0"); got != 1 {
		t.Errorf("v1.4.1 > v1.4.0 應回傳 1，實際：%d", got)
	}
}

// TestCompareVersions_PatchVersionOlder v1.4.0 < v1.4.1 。
func TestCompareVersions_PatchVersionOlder(t *testing.T) {
	if got := compareVersions("v1.4.0", "v1.4.1"); got != -1 {
		t.Errorf("v1.4.0 < v1.4.1 應回傳 -1，實際：%d", got)
	}
}

// TestCompareVersions_NonNumericSuffix 非數字後綴（如 v2.0-dev）不影響 semver 比較。
// v2.0-dev 中 "dev" 不是數字，commits 視為 0；但 semver 2.0 > 1.4 → 應回傳 1。
func TestCompareVersions_NonNumericSuffix(t *testing.T) {
	if got := compareVersions("v2.0-dev", "v1.4"); got != 1 {
		t.Errorf("v2.0-dev（semver 2.0）> v1.4 應回傳 1，實際：%d", got)
	}
}

// TestCompareVersions_RealScenario 使用者報告的實際場景：v1.4-48-g519be99 > v1.4-46-gd57936b。
func TestCompareVersions_RealScenario(t *testing.T) {
	if got := compareVersions("v1.4-48-g519be99", "v1.4-46-gd57936b"); got != 1 {
		t.Errorf("實際場景 v1.4-48 > v1.4-46 應回傳 1，實際：%d", got)
	}
}

// ── parseVersionParts 單元測試 ────────────────────────────────────────────

// TestParseVersionParts_SimpleVersion 解析純 semver 格式。
func TestParseVersionParts_SimpleVersion(t *testing.T) {
	p := parseVersionParts("v1.4")
	if len(p.nums) != 2 || p.nums[0] != 1 || p.nums[1] != 4 {
		t.Errorf("v1.4 → nums 應為 [1,4]，實際：%v", p.nums)
	}
	if p.commits != 0 {
		t.Errorf("v1.4 → commits 應為 0，實際：%d", p.commits)
	}
}

// TestParseVersionParts_GitDescribe 解析 git describe 格式。
func TestParseVersionParts_GitDescribe(t *testing.T) {
	p := parseVersionParts("v1.4-48-g519be99")
	if len(p.nums) != 2 || p.nums[0] != 1 || p.nums[1] != 4 {
		t.Errorf("v1.4-48-g... → nums 應為 [1,4]，實際：%v", p.nums)
	}
	if p.commits != 48 {
		t.Errorf("v1.4-48-g... → commits 應為 48，實際：%d", p.commits)
	}
}

// TestParseVersionParts_PatchVersion 解析三段式 semver。
func TestParseVersionParts_PatchVersion(t *testing.T) {
	p := parseVersionParts("v1.4.2-3-gabcdef0")
	if len(p.nums) != 3 || p.nums[2] != 2 {
		t.Errorf("v1.4.2-3-g... → nums 應為 [1,4,2]，實際：%v", p.nums)
	}
	if p.commits != 3 {
		t.Errorf("v1.4.2-3-g... → commits 應為 3，實際：%d", p.commits)
	}
}
