// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/sampler"
)

// getIPs returns the beacon IPs for each network
func getIPs(networkID uint32) []string {
	switch networkID {
	case constants.MainnetID:
		return []string{
			"54.71.41.185:9651",
		}
	case constants.FujiID:
		return []string{
			"3.214.61.227:9651",
			"52.206.218.4:9651",
			"44.194.128.146:9651",
			"3.143.146.90:9651",
			"3.142.66.84:9651",
			"3.142.32.15:9651",
			"44.240.251.247:9651",
			"44.224.22.217:9651",
			"52.13.58.52:9651",
			"18.163.142.196:9651",
			"16.162.54.143:9651",
			"18.167.153.71:9651",
			"52.29.183.160:9651",
			"18.159.63.226:9651",
			"3.65.152.247:9651",
			"34.247.100.96:9651",
			"34.250.89.215:9651",
			"54.228.143.65:9651",
			"54.232.253.20:9651",
			"54.94.159.80:9651",
			"54.94.242.98:9651",
		}
	default:
		return nil
	}
}

// getNodeIDs returns the beacon node IDs for each network
func getNodeIDs(networkID uint32) []string {
	switch networkID {
	case constants.MainnetID:
		return []string{
			"NodeID-5h3qTR82CKGR1heEnbNVvvhAWUMZZp3bm",
		}
	case constants.FujiID:
		return []string{
			"NodeID-2m38qc95mhHXtrhjyGbe7r2NhniqHHJRB",
			"NodeID-JjvzhxnLHLUQ5HjVRkvG827ivbLXPwA9u",
			"NodeID-LegbVf6qaMKcsXPnLStkdc1JVktmmiDxy",
			"NodeID-HGZ8ae74J3odT8ESreAdCtdnvWG1J4X5n",
			"NodeID-CYKruAjwH1BmV3m37sXNuprbr7dGQuJwG",
			"NodeID-4KXitMCoE9p2BHA6VzXtaTxLoEjNDo2Pt",
			"NodeID-LQwRLm4cbJ7T2kxcxp4uXCU5XD8DFrE1C",
			"NodeID-4CWTbdvgXHY1CLXqQNAp22nJDo5nAmts6",
			"NodeID-4QBwET5o8kUhvt9xArhir4d3R25CtmZho",
			"NodeID-JyE4P8f4cTryNV8DCz2M81bMtGhFFHexG",
			"NodeID-EDESh4DfZFC15i613pMtWniQ9arbBZRnL",
			"NodeID-BFa1padLXBj7VHa2JYvYGzcTBPQGjPhUy",
			"NodeID-CZmZ9xpCzkWqjAyS7L4htzh5Lg6kf1k18",
			"NodeID-FesGqwKq7z5nPFHa5iwZctHE5EZV9Lpdq",
			"NodeID-84KbQHSDnojroCVY7vQ7u9Tx7pUonPaS",
			"NodeID-CTtkcXvVdhpNp6f97LEUXPwsRD3A2ZHqP",
			"NodeID-hArafGhY2HFTbwaaVh1CSCUCUCiJ2Vfb",
			"NodeID-4B4rc5vdD1758JSBYL1xyvE5NHGzz6xzH",
			"NodeID-EzGaipqomyK9UKx9DBHV6Ky3y68hoknrF",
			"NodeID-NpagUxt6KQiwPch9Sd4osv8kD1TZnkjdk",
			"NodeID-3VWnZNViBP2b56QBY7pNJSLzN2rkTyqnK",
		}
	default:
		return nil
	}
}

// SampleBeacons returns the some beacons this node should connect to
func SampleBeacons(networkID uint32, count int) ([]string, []string) {
	ips := getIPs(networkID)
	ids := getNodeIDs(networkID)

	if numIPs := len(ips); numIPs < count {
		count = numIPs
	}

	sampledIPs := make([]string, 0, count)
	sampledIDs := make([]string, 0, count)

	s := sampler.NewUniform()
	_ = s.Initialize(uint64(len(ips)))
	indices, _ := s.Sample(count)
	for _, index := range indices {
		sampledIPs = append(sampledIPs, ips[int(index)])
		sampledIDs = append(sampledIDs, ids[int(index)])
	}

	return sampledIPs, sampledIDs
}
