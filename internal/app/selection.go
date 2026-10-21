package app

import (
	"sort"
	"time"
)

// Choose compares this round's valid sustained download rates. Old failures never
// outweigh a successful current measurement, and close results avoid IP churn.
func Choose(old Selection, results []Result, c Config, now time.Time) Selection {
	valid := []Result{}
	for _, r := range results {
		if r.OK && r.Kind == "throughput" && r.Speed > 0 && r.Family == "A" {
			valid = append(valid, r)
		}
	}
	sort.SliceStable(valid, func(i, j int) bool { return valid[i].Speed > valid[j].Speed })
	if len(results) == 0 {
		return old
	}
	base := Selection{RuleID: results[0].RuleID, Domain: results[0].Domain, Family: "A", ChangedAt: old.ChangedAt, Reason: "没有合格节点，使用默认解析", ValidUntil: now}
	var current *Result
	for i := range valid {
		if valid[i].IP == old.IP {
			current = &valid[i]
			break
		}
	}
	if old.Fixed {
		if current == nil {
			old.ValidUntil = now
			old.Reason = "固定节点检查失败，恢复默认解析"
			return old
		}
		old.Speed = current.Speed
		old.CheckedAt = now
		old.ValidUntil = now.Add(time.Duration(c.PolicySeconds) * time.Second)
		old.Reason = "固定节点已验证"
		return old
	}
	if len(valid) == 0 {
		return base
	}
	best := valid[0]
	if current != nil && old.ValidUntil.After(now) {
		old.Speed = current.Speed
		old.CheckedAt = now
		old.ValidUntil = now.Add(time.Duration(c.PolicySeconds) * time.Second)
		if best.IP == old.IP || best.Speed < current.Speed*(1+c.Improvement) {
			old.Reason = "当前节点已复测，速度接近，保留当前 IP"
			return old
		}
		if now.Sub(old.ChangedAt) < time.Duration(c.CooldownSeconds)*time.Second {
			old.Reason = "候选更快，切换冷却期间保留当前 IP"
			return old
		}
	}
	base.IP = best.IP
	base.Speed = best.Speed
	base.ChangedAt = now
	base.CheckedAt = now
	base.ValidUntil = now.Add(time.Duration(c.PolicySeconds) * time.Second)
	base.Reason = "本轮下载速度最快，已应用优选"
	return base
}
