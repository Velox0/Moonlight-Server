package main

// Get parent region from hierarchy
func getParentRegion(region string) string {
	cfg := getConfig()
	if cfg == nil || cfg.RegionHierarchy == nil {
		return ""
	}

	parent, ok := cfg.RegionHierarchy[region]
	if !ok {
		return ""
	}
	return parent
}

// Region filter
func filterClientsByRegion(targetRegion string) []*ClientInfo {
	clientsMutex.Lock()
	defer clientsMutex.Unlock()
	var matched []*ClientInfo
	for _, client := range clients {
		client.Mutex.Lock()
		if client.Valid && regionMatch(targetRegion, client.Region) {
			matched = append(matched, client)
		}
		client.Mutex.Unlock()
	}
	return matched
}

// Region matching (hierarchy aware)
func regionMatch(targetRegion, clientRegion string) bool {
	if targetRegion == "" || targetRegion == "default" || targetRegion == "global" {
		return true
	}
	r := clientRegion
	for r != "" {
		if r == targetRegion {
			return true
		}
		r = getParentRegion(r)
	}
	return false
}

func selectClientForRegion(region string) *ClientInfo {
	regionToTry := region
	for {
		candidates := filterClientsByRegion(regionToTry)
		if len(candidates) > 0 {
			return selectBestClient(candidates)
		}
		if regionToTry == "" || regionToTry == "default" || regionToTry == "global" {
			break
		}
		regionToTry = getParentRegion(regionToTry)
	}
	return selectBestClient(getAllValidClients())
}

// Utility to list all valid clients
func getAllValidClients() []*ClientInfo {
	clientsMutex.Lock()
	defer clientsMutex.Unlock()
	var valid []*ClientInfo
	for _, c := range clients {
		c.Mutex.Lock()
		if c.Valid {
			valid = append(valid, c)
		}
		c.Mutex.Unlock()
	}
	return valid
}
