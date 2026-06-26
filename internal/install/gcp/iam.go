package gcp

// RuntimeIAMPermissions returns the permissions required by betternat-agent
// for the experimental GCP HA path.
func RuntimeIAMPermissions() []string {
	return []string{
		"compute.addresses.get",
		"compute.addresses.use",
		"compute.globalOperations.get",
		"compute.instances.addAccessConfig",
		"compute.instances.deleteAccessConfig",
		"compute.instances.get",
		"compute.instances.use",
		"compute.networks.get",
		"compute.networks.updatePolicy",
		"compute.routes.create",
		"compute.routes.delete",
		"compute.routes.get",
		"compute.subnetworks.useExternalIp",
		"compute.zoneOperations.get",
		"datastore.databases.get",
		"datastore.databases.getMetadata",
		"datastore.databases.list",
		"datastore.entities.allocateIds",
		"datastore.entities.create",
		"datastore.entities.delete",
		"datastore.entities.get",
		"datastore.entities.list",
		"datastore.entities.update",
		"datastore.namespaces.get",
		"datastore.namespaces.list",
		"datastore.schemas.list",
		"datastore.statistics.get",
		"datastore.statistics.list",
		"resourcemanager.projects.get",
	}
}
