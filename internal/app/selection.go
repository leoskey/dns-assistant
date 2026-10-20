package app

import (
	"sort"
	"time"
)

func Choose(old Selection, results []Result, c Config, now time.Time) Selection {
	valid := []Result{}
	for _, r := range results {
		if r.OK && r.Kind == "throughput" && r.Speed > 0 {
			valid = append(valid, r)
		}
	}
	sort.SliceStable(valid, func(i, j int) bool { return valid[i].Speed > valid[j].Speed })
	if len(results) == 0 {
		return old
	}
	base := Selection{RuleID: results[0].RuleID, Domain: results[0].Domain, Family: results[0].Family, ChangedAt: old.ChangedAt}
	find := func(ip string) *Result {
		for i := range valid {
			if valid[i].IP == ip {
				return &valid[i]
			}
		}
		return nil
	}
	current := find(old.IP)
	if old.Fixed {
		if current == nil {
			old.ValidUntil = now
			old.Backup = ""
			old.Reason = "固定节点检查失败，恢复上游解析"
			return old
		}
		old.Speed = current.Speed
		old.ValidUntil = now.Add(time.Duration(c.PolicySeconds) * time.Second)
		old.Reason = "固定节点已验证"
		return old
	}
	if len(valid) == 0 {
		base.Reason = "没有合格节点，使用上游解析"
		base.ValidUntil = now
		return base
	}
	best := valid[0]
	apply := func(r Result, reason string) Selection {
		v := base
		v.IP = r.IP
		v.Speed = r.Speed
		v.ValidUntil = now.Add(time.Duration(c.PolicySeconds) * time.Second)
		v.ChangedAt = now
		v.Reason = reason
		for _, x := range valid {
			if x.IP != r.IP {
				v.Backup = x.IP
				break
			}
		}
		return v
	}
	if current == nil || !old.ValidUntil.After(now) {
		return apply(best, "自动选择当前有效节点")
	}
	old.Speed = current.Speed
	old.ValidUntil = now.Add(time.Duration(c.PolicySeconds) * time.Second)
	old.Backup = ""
	for _, r := range valid {
		if r.IP != old.IP {
			old.Backup = r.IP
			break
		}
	}
	if best.IP != old.IP && best.Speed >= old.Speed*(1+c.Improvement) {
		if old.Challenger == best.IP {
			old.Streak++
		} else {
			old.Challenger = best.IP
			old.Streak = 1
		}
		if old.Streak >= 2 && now.Sub(old.ChangedAt) >= time.Duration(c.CooldownSeconds)*time.Second {
			return apply(best, "新节点连续两轮表现更好")
		}
		old.Reason = "候选更快，等待复测确认"
	} else {
		old.Challenger = ""
		old.Streak = 0
		old.Reason = "当前节点保持稳定"
	}
	return old
}
