// Package rating implements node reputation/rating (aligned with PlatariumCore node_registry.rs).
// Formula: Reputation = (Uptime×300 + Latency×200 + VoteAccuracy×300 + StakeWeight×200) / 1000.
package rating

import (
	"math/rand"
	"sync"
)

const (
	// ScoreScale: 0..=ScoreScale represents 0.0..=1.0 (as in Core).
	ScoreScale = 1_000_000
	// Weights (sum 1000): Uptime 30%, Latency 20%, VoteAccuracy 30%, Stake 20%.
	WeightUptime       = 300
	WeightLatency      = 200
	WeightVoteAccuracy = 300
	WeightStake        = 200
)

// NodeScores holds per-node scores for reputation (Core-compatible).
type NodeScores struct {
	NodeID          string `json:"nodeId"`
	UptimeScore     int64  `json:"uptimeScore"`     // 0..=ScoreScale
	LatencyScore    int64  `json:"latencyScore"`    // 0..=ScoreScale (1.0 = best)
	MissedVotes     int64  `json:"missedVotes"`
	TotalVotes      int64  `json:"totalVotes"`
	Stake           int64  `json:"stake"`
	ReputationScore int64  `json:"reputationScore"` // 0..=ScoreScale
	LoadScore       int64  `json:"loadScore"`       // 0..=ScoreScale (current/max)
	CurrentTasks    int64  `json:"currentTasks"`
	MaxCapacity     int64  `json:"maxCapacity"`
}

// VoteAccuracy returns (totalVotes - missedVotes) / totalVotes in 0..=ScoreScale, or ScoreScale if no votes.
func (n *NodeScores) VoteAccuracy() int64 {
	if n.TotalVotes <= 0 {
		return ScoreScale
	}
	correct := n.TotalVotes - n.MissedVotes
	if correct < 0 {
		correct = 0
	}
	return correct * ScoreScale / n.TotalVotes
}

// ComputeReputation recomputes ReputationScore. maxStake is the max stake across all nodes (for StakeWeight).
func (n *NodeScores) ComputeReputation(maxStake int64) {
	voteAcc := n.VoteAccuracy()
	var stakeWeight int64 = ScoreScale
	if maxStake > 0 && n.Stake >= 0 {
		w := n.Stake * ScoreScale / maxStake
		if w < int64(ScoreScale) {
			stakeWeight = w
		}
	}
	sum := n.UptimeScore*int64(WeightUptime) + n.LatencyScore*int64(WeightLatency) +
		voteAcc*int64(WeightVoteAccuracy) + stakeWeight*int64(WeightStake)
	n.ReputationScore = sum / 1000
}

// SelectionWeight returns reputation × (1 - load) as in Core (legacy). Load 0 = full weight.
func (n *NodeScores) SelectionWeight() int64 {
	loadPenalty := int64(ScoreScale) - n.LoadScore
	if loadPenalty < 0 {
		loadPenalty = 0
	}
	return n.ReputationScore * loadPenalty / ScoreScale
}

// Registry stores node scores and computes ratings.
type Registry struct {
	mu    sync.RWMutex
	nodes map[string]*NodeScores
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{nodes: make(map[string]*NodeScores)}
}

// EnsureNode adds or returns existing node with default scores (1.0 except load 0).
func (r *Registry) EnsureNode(nodeID string, stake, maxCapacity int64) *NodeScores {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n, ok := r.nodes[nodeID]; ok {
		return n
	}
	if maxCapacity <= 0 {
		maxCapacity = 1
	}
	n := &NodeScores{
		NodeID:       nodeID,
		UptimeScore:  ScoreScale,
		LatencyScore: ScoreScale,
		Stake:        stake,
		LoadScore:    0,
		CurrentTasks: 0,
		MaxCapacity:  maxCapacity,
	}
	n.ComputeReputation(0)
	r.nodes[nodeID] = n
	return n
}

// Get returns node scores by ID, or nil.
func (r *Registry) Get(nodeID string) *NodeScores {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.nodes[nodeID]
}

// SetVoteStats updates missed/total votes for a node and recomputes reputation.
func (r *Registry) SetVoteStats(nodeID string, missed, total int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.nodes[nodeID]
	if n == nil {
		return
	}
	n.MissedVotes = missed
	if n.MissedVotes < 0 {
		n.MissedVotes = 0
	}
	n.TotalVotes = total
	if n.TotalVotes < n.MissedVotes {
		n.TotalVotes = n.MissedVotes
	}
	maxStake := r.maxStakeLocked()
	n.ComputeReputation(maxStake)
}

// SetUptime sets uptime score (0..=ScoreScale). Used e.g. on disconnect penalty.
func (r *Registry) SetUptime(nodeID string, score int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.nodes[nodeID]
	if n == nil {
		return
	}
	if score < 0 {
		score = 0
	}
	if score > ScoreScale {
		score = ScoreScale
	}
	n.UptimeScore = score
	maxStake := r.maxStakeLocked()
	n.ComputeReputation(maxStake)
}

// ApplyUptimePenalty multiplies current uptime by factor (e.g. 0.95 for 5% penalty).
func (r *Registry) ApplyUptimePenalty(nodeID string, factor float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.nodes[nodeID]
	if n == nil {
		return
	}
	n.UptimeScore = int64(float64(n.UptimeScore) * factor)
	if n.UptimeScore < 0 {
		n.UptimeScore = 0
	}
	if n.UptimeScore > ScoreScale {
		n.UptimeScore = ScoreScale
	}
	maxStake := r.maxStakeLocked()
	n.ComputeReputation(maxStake)
}

// SetLoad sets current_tasks and max_capacity, then LoadScore = current_tasks×ScoreScale/max_capacity (Core: вплив навантаження на вагу вибору).
// SelectionWeight = reputation×(1−load); висока навантаженість знижує ймовірність вибору як пропонера/конфірмера.
func (r *Registry) SetLoad(nodeID string, currentTasks, maxCapacity int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.nodes[nodeID]
	if n == nil {
		return
	}
	n.CurrentTasks = currentTasks
	if currentTasks < 0 {
		n.CurrentTasks = 0
	}
	if maxCapacity <= 0 {
		maxCapacity = 1
	}
	n.MaxCapacity = maxCapacity
	n.LoadScore = n.CurrentTasks * ScoreScale / n.MaxCapacity
	if n.LoadScore < 0 {
		n.LoadScore = 0
	}
	if n.LoadScore > ScoreScale {
		n.LoadScore = ScoreScale
	}
}

func (r *Registry) maxStakeLocked() int64 {
	var max int64
	for _, n := range r.nodes {
		if n.Stake > max {
			max = n.Stake
		}
	}
	return max
}

// RecomputeAll recalculates reputation for all nodes (e.g. after stake change).
func (r *Registry) RecomputeAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	maxStake := r.maxStakeLocked()
	for _, n := range r.nodes {
		n.ComputeReputation(maxStake)
	}
}

// All returns a copy of all node scores (for API).
func (r *Registry) All() []NodeScores {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]NodeScores, 0, len(r.nodes))
	for _, n := range r.nodes {
		out = append(out, *n)
	}
	return out
}

// SelectionWeightFor returns SelectionWeight for nodeID from registry, or ScoreScale if not registered.
func (r *Registry) SelectionWeightFor(nodeID string) int64 {
	r.mu.RLock()
	n := r.nodes[nodeID]
	r.mu.RUnlock()
	if n == nil {
		return ScoreScale
	}
	w := n.SelectionWeight()
	if w <= 0 {
		return 1
	}
	return w
}

// SelectionPercentFromLoad returns the percentage of validators to select (Core: чим вище навантаження, тим менше валідаторів).
// loadPct 0→50%; 1–19→30%; 20–29→25%; 30–59→20%; 60–84→15%; else 10%. Fallback when Core unavailable.
func SelectionPercentFromLoad(loadPct int) int {
	if loadPct == 0 {
		return 50
	}
	if loadPct < 20 {
		return 30
	}
	if loadPct < 30 {
		return 25
	}
	if loadPct < 60 {
		return 20
	}
	if loadPct < 85 {
		return 15
	}
	return 10
}

// SelectCommittee returns up to `count` node IDs from candidates, weighted by SelectionWeight (without replacement, as in Core).
// Used to form the voting committee: when network is loaded, fewer nodes participate so rounds finish faster.
func (r *Registry) SelectCommittee(candidateNodeIDs []string, count int) []string {
	if count <= 0 || len(candidateNodeIDs) == 0 {
		return nil
	}
	if count >= len(candidateNodeIDs) {
		return append([]string(nil), candidateNodeIDs...)
	}
	// Weighted selection without replacement: pick one, remove from pool, repeat.
	remaining := make([]string, len(candidateNodeIDs))
	copy(remaining, candidateNodeIDs)
	result := make([]string, 0, count)
	for len(result) < count && len(remaining) > 0 {
		picked := r.SelectByWeight(remaining)
		if picked == "" {
			break
		}
		result = append(result, picked)
		// Remove picked from remaining
		newRemaining := remaining[:0]
		for _, id := range remaining {
			if id != picked {
				newRemaining = append(newRemaining, id)
			}
		}
		remaining = newRemaining
	}
	return result
}

// SelectByWeight picks one node from candidates with probability proportional to reputation/weight (protocol selection for L1/L2).
func (r *Registry) SelectByWeight(candidateNodeIDs []string) string {
	if len(candidateNodeIDs) == 0 {
		return ""
	}
	if len(candidateNodeIDs) == 1 {
		return candidateNodeIDs[0]
	}
	var total int64
	weights := make([]int64, len(candidateNodeIDs))
	for i, id := range candidateNodeIDs {
		w := r.SelectionWeightFor(id)
		if w <= 0 {
			w = 1
		}
		weights[i] = w
		total += w
	}
	if total <= 0 {
		return candidateNodeIDs[rand.Intn(len(candidateNodeIDs))]
	}
	roll := rand.Int63n(total)
	var sum int64
	for i, w := range weights {
		sum += w
		if roll < sum {
			return candidateNodeIDs[i]
		}
	}
	return candidateNodeIDs[len(candidateNodeIDs)-1]
}
