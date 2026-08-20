package gateway

// ByObservedLatency scores lower observed latency as better.
func ByObservedLatency() Scorer {
	return ScoreFunc(func(_ Request, candidate Candidate) float64 {
		return float64(candidate.observedLatency)
	})
}

// ByInFlight scores fewer concurrent provider calls as better.
func ByInFlight() Scorer {
	return ScoreFunc(func(_ Request, candidate Candidate) float64 {
		return float64(candidate.inFlight)
	})
}

// ByCost scores lower configured cost as better.
func ByCost() Scorer {
	return ScoreFunc(func(_ Request, candidate Candidate) float64 {
		return candidate.cost
	})
}

// ByFailureRate scores lower observed local failure ratio as better.
func ByFailureRate() Scorer {
	return ScoreFunc(func(_ Request, candidate Candidate) float64 {
		return candidate.FailureRate()
	})
}

// LowestLatency selects the provider with the lowest observed latency.
func LowestLatency() RoutingStrategy { return Least(ByObservedLatency()) }

// LeastBusy selects the provider with the fewest in-flight requests.
func LeastBusy() RoutingStrategy { return Least(ByInFlight()) }

// LowestCost selects the provider with the lowest configured cost.
func LowestCost() RoutingStrategy { return Least(ByCost()) }
