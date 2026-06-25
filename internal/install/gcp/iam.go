package gcp

// RuntimeIAMPermissions returns the permissions required by betternat-agent
// for the experimental GCP route-only HA path.
func RuntimeIAMPermissions() []string {
	return []string{
		"compute.globalOperations.get",
		"compute.instances.get",
		"compute.instances.use",
		"compute.networks.get",
		"compute.routes.create",
		"compute.routes.delete",
		"compute.routes.get",
		"datastore.databases.get",
		"datastore.entities.create",
		"datastore.entities.delete",
		"datastore.entities.get",
		"datastore.entities.list",
		"datastore.entities.update",
	}
}
