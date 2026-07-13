package discovery

import (
	"errors"
	"sort"

	"gorm.io/gorm"
)

type SiteGraphEdgeView struct {
	Edge DiscoverySiteEdge `json:"edge"`
	From DiscoverySite     `json:"from"`
	To   DiscoverySite     `json:"to"`
}

type SiteGraphPath struct {
	Sites []DiscoverySite     `json:"sites"`
	Edges []DiscoverySiteEdge `json:"edges"`
}

type SiteGraphView struct {
	Site                    DiscoverySite       `json:"site"`
	Inbound                 []SiteGraphEdgeView `json:"inbound"`
	Outbound                []SiteGraphEdgeView `json:"outbound"`
	ShortestSeedPath        SiteGraphPath       `json:"shortestSeedPath"`
	GraphDepth              int                 `json:"graphDepth"`
	IndependentInboundSites int64               `json:"independentInboundSites"`
	StopReason              string              `json:"stopReason"`
	StopDetails             string              `json:"stopDetails"`
}

func GetSiteGraph(siteID uint) (*SiteGraphView, error) {
	if db == nil {
		return nil, gorm.ErrRecordNotFound
	}
	var site DiscoverySite
	if err := db.First(&site, siteID).Error; err != nil {
		return nil, err
	}
	var edges []DiscoverySiteEdge
	if err := db.Where("active = ? AND (from_site_id = ? OR to_site_id = ?)", true, siteID, siteID).Order("id").Find(&edges).Error; err != nil {
		return nil, err
	}
	siteIDs := make(map[uint]struct{})
	for _, edge := range edges {
		siteIDs[edge.FromSiteID] = struct{}{}
		siteIDs[edge.ToSiteID] = struct{}{}
	}
	var related []DiscoverySite
	ids := make([]uint, 0, len(siteIDs))
	for id := range siteIDs {
		ids = append(ids, id)
	}
	if len(ids) > 0 {
		if err := db.Where("id IN ?", ids).Find(&related).Error; err != nil {
			return nil, err
		}
	}
	byID := map[uint]DiscoverySite{site.ID: site}
	for _, item := range related {
		byID[item.ID] = item
	}
	view := &SiteGraphView{
		Site: site, Inbound: make([]SiteGraphEdgeView, 0), Outbound: make([]SiteGraphEdgeView, 0),
		GraphDepth: site.GraphDepth, StopReason: site.OperationalPause, StopDetails: site.OperationalDetails,
	}
	for _, edge := range edges {
		item := SiteGraphEdgeView{Edge: edge, From: byID[edge.FromSiteID], To: byID[edge.ToSiteID]}
		if edge.ToSiteID == siteID {
			view.Inbound = append(view.Inbound, item)
		}
		if edge.FromSiteID == siteID {
			view.Outbound = append(view.Outbound, item)
		}
	}
	if err := db.Model(&DiscoverySiteEdge{}).Where("to_site_id = ? AND active = ?", siteID, true).Distinct("from_site_id").Count(&view.IndependentInboundSites).Error; err != nil {
		return nil, err
	}
	path, err := shortestSeedPath(siteID)
	if err != nil {
		return nil, err
	}
	view.ShortestSeedPath = path
	return view, nil
}

func shortestSeedPath(targetID uint) (SiteGraphPath, error) {
	var sites []DiscoverySite
	if err := db.Order("id").Find(&sites).Error; err != nil {
		return SiteGraphPath{}, err
	}
	byID := make(map[uint]DiscoverySite, len(sites))
	seeds := make([]uint, 0)
	for _, site := range sites {
		byID[site.ID] = site
		if site.Status == DiscoverySiteStatusSeed {
			seeds = append(seeds, site.ID)
		}
	}
	sort.Slice(seeds, func(i, j int) bool { return seeds[i] < seeds[j] })
	var edges []DiscoverySiteEdge
	if err := db.Where("active = ?", true).Order("id").Find(&edges).Error; err != nil {
		return SiteGraphPath{}, err
	}
	adjacency := make(map[uint][]DiscoverySiteEdge)
	for _, edge := range edges {
		adjacency[edge.FromSiteID] = append(adjacency[edge.FromSiteID], edge)
	}
	queue := append([]uint(nil), seeds...)
	visited := make(map[uint]bool, len(queue))
	previousSite := make(map[uint]uint)
	previousEdge := make(map[uint]DiscoverySiteEdge)
	for _, seed := range seeds {
		visited[seed] = true
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == targetID {
			break
		}
		for _, edge := range adjacency[current] {
			if visited[edge.ToSiteID] {
				continue
			}
			visited[edge.ToSiteID] = true
			previousSite[edge.ToSiteID] = current
			previousEdge[edge.ToSiteID] = edge
			queue = append(queue, edge.ToSiteID)
		}
	}
	if !visited[targetID] {
		return SiteGraphPath{Sites: []DiscoverySite{}, Edges: []DiscoverySiteEdge{}}, nil
	}
	reverseSites := []DiscoverySite{byID[targetID]}
	reverseEdges := make([]DiscoverySiteEdge, 0)
	for current := targetID; ; {
		previous, found := previousSite[current]
		if !found {
			break
		}
		reverseEdges = append(reverseEdges, previousEdge[current])
		reverseSites = append(reverseSites, byID[previous])
		current = previous
	}
	path := SiteGraphPath{Sites: make([]DiscoverySite, len(reverseSites)), Edges: make([]DiscoverySiteEdge, len(reverseEdges))}
	for index := range reverseSites {
		path.Sites[len(reverseSites)-1-index] = reverseSites[index]
	}
	for index := range reverseEdges {
		path.Edges[len(reverseEdges)-1-index] = reverseEdges[index]
	}
	return path, nil
}

func IsSiteGraphNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}
