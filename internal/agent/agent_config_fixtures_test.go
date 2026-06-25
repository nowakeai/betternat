package agent

func validConfigJSON() string {
	return `{
	  "version": "v0",
	  "gateway_id": "prod-egress",
	  "ha_group_id": "prod-egress-a",
	  "cloud": "aws",
	  "region": "us-west-2",
	  "local": {"primary_interface": "ens5"},
	  "datapath": {
	    "engine": "loxilb",
	    "fallback_engine": "nftables",
	    "private_cidrs": ["10.0.0.0/8"],
	    "loxilb": {
	      "api_address": "127.0.0.1",
	      "api_port": 11111,
	      "snat_to": "auto",
	      "snat_interface": "ens5"
	    }
	  },
	  "ha": {},
	  "observability": {},
	  "rollback": {}
	}`
}

func validHAConfigJSON() string {
	return `{
	  "version": "v0",
	  "gateway_id": "prod-egress",
	  "ha_group_id": "prod-egress-a",
	  "cloud": "aws",
	  "region": "us-west-2",
	  "local": {"instance_id":"i-local","primary_interface": "ens5"},
	  "datapath": {
	    "engine": "loxilb",
	    "fallback_engine": "nftables",
	    "private_cidrs": ["10.0.0.0/8"],
	    "loxilb": {
	      "api_address": "127.0.0.1",
	      "api_port": 11111,
	      "snat_to": "auto",
	      "snat_interface": "ens5"
	    }
	  },
	  "ha": {
	    "enabled": true,
	    "lease": {
	      "backend": "dynamodb",
	      "table": "betternat-test-leases",
	      "key": "prod-egress-a",
	      "ttl_seconds": 10,
	      "renew_interval_seconds": 3
	    },
	    "route_failover": {
	      "mode": "replace_route",
	      "route_table_ids": ["rtb-a"],
	      "destination_cidr": "0.0.0.0/0",
	      "target_type": "instance"
	    },
	    "public_identity": {
	      "mode": "shared_eip",
	      "allocation_id": "eipalloc-123"
	    }
	  },
	  "observability": {},
	  "rollback": {}
	}`
}

func validGCPHAConfigJSON() string {
	return `{
	  "version": "v0",
	  "gateway_id": "prod-egress",
	  "ha_group_id": "prod-egress-a",
	  "cloud": "gcp",
	  "region": "us-west2",
	  "gcp": {
	    "project_id": "shared-resources-alt",
	    "zone": "us-west2-a",
	    "network": "prod-vpc",
	    "client_tag": "private-client",
	    "route_priority": 800,
	    "firestore_database_id": "betternat-test"
	  },
	  "local": {"node_id":"gce-a","primary_interface": "ens4"},
	  "datapath": {
	    "engine": "nftables",
	    "fallback_engine": "nftables",
	    "private_cidrs": ["10.0.0.0/8"]
	  },
	  "ha": {
	    "enabled": true,
	    "lease": {
	      "backend": "firestore",
	      "key": "prod-egress-a",
	      "ttl_seconds": 10,
	      "renew_interval_seconds": 3
	    },
	    "route_failover": {
	      "mode": "replace_route",
	      "route_table_ids": ["prod-default-via-gateway"],
	      "destination_cidr": "0.0.0.0/0",
	      "target_type": "instance"
	    },
	    "public_identity": {}
	  },
	  "coordination": {
	    "backend": "firestore",
	    "registry_refresh_interval_seconds": 2,
	    "registry_ttl_seconds": 20,
	    "handover_ttl_seconds": 3600
	  },
	  "observability": {},
	  "rollback": {}
	}`
}
