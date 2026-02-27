package grpc_test

// ApplicationAuthJSONCS3 is the configuration for the applicationauth service
var ApplicationAuthJSONCS3 = map[string]any{
	"shared": map[string]any{
		"jwt_secret": "secret",
	},
	"log": map[string]any{
		"level": "debug",
	},
	"grpc": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"applicationauth": map[string]any{
				"driver": "jsoncs3",
				"drivers": map[string]any{
					"jsoncs3": map[string]any{
						"provider_addr":                 "{{storage_system_address}}",
						"service_user_id":               "service-user-id",
						"service_user_idp":              "internal",
						"machine_auth_apikey":           "secret",
						"utime_update_interval_seconds": -1,
						"update_retry_count":            25,
					},
				},
			},
		},
	},
}

// StorageSystem is the configuration for the storage system
var StorageSystem = map[string]any{
	"shared": map[string]any{
		"jwt_secret": "secret",
	},
	"log": map[string]any{
		"level": "debug",
	},
	"grpc": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"gateway": map[string]any{
				"authregistrysvc":                "{{grpc_address}}",
				"storageregistrysvc":             "{{grpc_address}}",
				"userprovidersvc":                "{{grpc_address}}",
				"groupprovidersvc":               "{{grpc_address}}",
				"permissionssvc":                 "{{grpc_address}}",
				"cache_store":                    "noop",
				"cache_database":                 "system",
				"disable_home_creation_on_login": true,
			},
			"userprovider": map[string]any{
				"driver": "memory",
				"drivers": map[string]any{
					"memory": map[string]any{
						"users": map[string]any{
							"serviceuser": map[string]any{
								"id": map[string]any{
									"opaqueId": "service-user-id",
									"idp":      "internal",
									"type":     3,
								},
								"username":     "serviceuser",
								"display_name": "System User",
							},
						},
					},
				},
			},
			"authregistry": map[string]any{
				"driver": "static",
				"drivers": map[string]any{
					"static": map[string]any{
						"rules": map[string]any{
							"machine": "{{grpc_address}}",
						},
					},
				},
			},
			"authprovider": map[string]any{
				"auth_manager": "machine",
				"auth_managers": map[string]any{
					"machine": map[string]any{
						"api_key":      "secret",
						"gateway_addr": "{{grpc_address}}",
					},
				},
			},
			"permissions": map[string]any{
				"driver": "demo",
			},
			"storageregistry": map[string]any{
				"driver": "static",
				"drivers": map[string]any{
					"static": map[string]any{
						"rules": map[string]any{
							"/": map[string]any{
								"address": "{{grpc_address}}",
							},
						},
					},
				},
			},
			"storageprovider": map[string]any{
				"driver":          "decomposed",
				"data_server_url": "http://{{grpc_address+1}}/data",
				"drivers": map[string]any{
					"decomposed": map[string]any{
						"root":               "{{root}}/storage",
						"permissionssvc":     "{{grpc_address}}",
						"disable_versioning": true,
						"filemetadatacache": map[string]any{
							"cache_table": "{{grpc_address}}",
						},
					},
				},
			},
		},
	},
	"http": map[string]any{
		"address": "{{grpc_address+1}}",
		"services": map[string]any{
			"dataprovider": map[string]any{
				"prefix": "data",
				"driver": "decomposed",
				"drivers": map[string]any{
					"decomposed": map[string]any{
						"root":               "{{root}}/storage",
						"permissionssvc":     "{{grpc_address}}",
						"disable_versioning": true,
						"filemetadatacache": map[string]any{
							"cache_table": "{{grpc_address}}",
						},
					},
				},
				"data_txs": map[string]any{
					"simple": map[string]any{
						"cache_store":    "noop",
						"cache_database": "system",
						"cache_table":    "stat",
					},
					"tus": map[string]any{
						"cache_store":    "noop",
						"cache_database": "system",
						"cache_table":    "stat",
					},
					"spaces": map[string]any{
						"cache_store":    "noop",
						"cache_database": "system",
						"cache_table":    "stat",
					},
				},
			},
		},
	},
}

// GatewaySingleStorage is the configuration for the gateway with single storage
var GatewaySingleStorage = map[string]any{
	"shared": map[string]any{
		"jwt_secret": "changemeplease",
		"gatewaysvc": "{{grpc_address}}",
	},
	"grpc": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"gateway": map[string]any{
				"storageregistrysvc":     "{{grpc_address}}",
				"userprovidersvc":        "{{users_address}}",
				"stat_cache_ttl":         1,
				"permissionssvc":         "{{permissions_address}}",
				"usershareprovidersvc":   "{{shares_address}}",
				"publicshareprovidersvc": "{{shares_address}}",
			},
			"authregistry": map[string]any{
				"driver": "static",
				"drivers": map[string]any{
					"static": map[string]any{
						"rules": map[string]any{
							"basic":        "{{users_address}}",
							"publicshares": "{{shares_address}}",
						},
					},
				},
			},
			"storageregistry": map[string]any{
				"driver": "spaces",
				"drivers": map[string]any{
					"spaces": map[string]any{
						"home_template": "/users/{{.Id.OpaqueId}}",
						"providers": map[string]any{
							"{{storage_address}}": map[string]any{
								"spaces": map[string]any{
									"personal": map[string]any{
										"mount_point":   "/users",
										"path_template": "/users/{{.Space.Owner.Id.OpaqueId}}",
									},
									"project": map[string]any{
										"mount_point":   "/users/[^/]+/Projects",
										"path_template": "/users/{{.CurrentUser.Id.OpaqueId}}/Projects/{{.Space.Name}}",
									},
								},
							},
							"{{storage_publiclink_address}}": map[string]any{
								"providerid": "7993447f-687f-490d-875c-ac95e89a62a4",
								"spaces": map[string]any{
									"grant": map[string]any{
										"mount_point": ".",
									},
									"mountpoint": map[string]any{
										"mount_point":   "/public",
										"path_template": "/public/{{.Space.Root.OpaqueId}}",
									},
								},
							},
						},
					},
				},
			},
		},
	},
	"http": map[string]any{
		"address": "{{grpc_address+1}}",
		"services": map[string]any{
			"datagateway": map[string]any{
				"transfer_shared_secret": "replace-me-with-a-transfer-secret",
			},
		},
	},
}

// UserProviderJSON is the configuration for the user provider
var UserProviderJSON = map[string]any{
	"grpc": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"userprovider": map[string]any{
				"driver": "json",
				"drivers": map[string]any{
					"json": map[string]any{
						"users": "fixtures/users.demo.json",
					},
				},
			},
		},
	},
}

// StorageProviderDecomposed is the configuration for the storage provider
var StorageProviderDecomposed = map[string]any{
	"shared": map[string]any{
		"jwt_secret": "changemeplease",
		"gatewaysvc": "{{gateway_address}}",
	},
	"grpc": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"storageprovider": map[string]any{
				"driver": "decomposed",
				"drivers": map[string]any{
					"decomposed": map[string]any{
						"root":                "{{root}}/storage",
						"treetime_accounting": true,
						"treesize_accounting": true,
						"permissionssvc":      "{{permissions_address}}",
						"filemetadatacache": map[string]any{
							"cache_store": "noop",
						},
					},
				},
			},
		},
	},
}

// StoragePublicLink is the configuration for the public link storage
var StoragePublicLink = map[string]any{
	"shared": map[string]any{
		"jwt_secret": "changemeplease",
		"gatewaysvc": "{{gateway_address}}",
	},
	"log": map[string]any{
		"level": "error",
	},
	"grpc": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"publicstorageprovider": map[string]any{
				"gateway_addr": "{{gateway_address}}",
				"mount_id":     "7993447f-687f-490d-875c-ac95e89a62a4",
			},
		},
	},
}

// PermissionsOpenCloudCI is the configuration for the permissions service
var PermissionsOpenCloudCI = map[string]any{
	"shared": map[string]any{
		"jwt_secret": "changemeplease",
	},
	"grpc": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"permissions": map[string]any{
				"driver": "demo",
				"drivers": map[string]any{
					"ci": map[string]any{},
				},
			},
		},
	},
}

// Shares is the configuration for the shares service
var Shares = map[string]any{
	"shared": map[string]any{
		"jwt_secret": "changemeplease",
		"gatewaysvc": "{{gateway_address}}",
	},
	"log": map[string]any{
		"level": "error",
	},
	"grpc": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"usershareprovider": map[string]any{
				"driver": "memory",
			},
			"authprovider": map[string]any{
				"auth_manager": "publicshares",
				"auth_managers": map[string]any{
					"publicshares": map[string]any{
						"gateway_addr": "{{gateway_address}}",
					},
				},
			},
			"publicshareprovider": map[string]any{
				"driver": "json",
				"drivers": map[string]any{
					"json": map[string]any{
						"file":         "{{root}}/publicshares.json",
						"gateway_addr": "{{gateway_address}}",
					},
				},
			},
		},
	},
}

// Gateway is the configuration for the gateway
var Gateway = map[string]any{
	"shared": map[string]any{
		"jwt_secret": "changemeplease",
		"gatewaysvc": "{{grpc_address}}",
	},
	"grpc": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"gateway": map[string]any{
				"storageregistrysvc":     "{{grpc_address}}",
				"userprovidersvc":        "{{users_address}}",
				"stat_cache_ttl":         1,
				"permissionssvc":         "{{permissions_address}}",
				"usershareprovidersvc":   "{{shares_address}}",
				"publicshareprovidersvc": "{{shares_address}}",
			},
			"authregistry": map[string]any{
				"driver": "static",
				"drivers": map[string]any{
					"static": map[string]any{
						"rules": map[string]any{
							"basic":        "{{users_address}}",
							"publicshares": "{{shares_address}}",
						},
					},
				},
			},
			"storageregistry": map[string]any{
				"driver": "spaces",
				"drivers": map[string]any{
					"spaces": map[string]any{
						"home_template": "/users/{{.Id.OpaqueId}}",
						"providers": map[string]any{
							"{{storage_address}}": map[string]any{
								"spaces": map[string]any{
									"personal": map[string]any{
										"mount_point":   "/users",
										"path_template": "/users/{{.Space.Owner.Id.OpaqueId}}",
									},
								},
							},
							"{{storage2_address}}": map[string]any{
								"spaces": map[string]any{
									"project": map[string]any{
										"mount_point":   "/users/[^/]+/Projects",
										"path_template": "/users/{{.CurrentUser.Id.OpaqueId}}/Projects/{{.Space.Name}}",
									},
								},
							},
							"{{storage_publiclink_address}}": map[string]any{
								"providerid": "7993447f-687f-490d-875c-ac95e89a62a4",
								"spaces": map[string]any{
									"grant": map[string]any{
										"mount_point": ".",
									},
									"mountpoint": map[string]any{
										"mount_point":   "/public",
										"path_template": "/public/{{.Space.Root.OpaqueId}}",
									},
								},
							},
						},
					},
				},
			},
		},
	},
	"http": map[string]any{
		"address": "{{grpc_address+1}}",
		"services": map[string]any{
			"datagateway": map[string]any{
				"transfer_shared_secret": "replace-me-with-a-transfer-secret",
			},
		},
	},
}

// GatewayStatic is the configuration for the static gateway
var GatewayStatic = map[string]any{
	"shared": map[string]any{
		"jwt_secret": "changemeplease",
		"gatewaysvc": "{{grpc_address}}",
	},
	"grpc": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"gateway": map[string]any{
				"storageregistrysvc": "{{grpc_address}}",
				"stat_cache_ttl":     1,
				"permissionssvc":     "{{permissions_address}}",
			},
			"authregistry": map[string]any{
				"driver": "static",
				"drivers": map[string]any{
					"static": map[string]any{
						"rules": map[string]any{
							"basic": "{{users_address}}",
						},
					},
				},
			},
			"storageregistry": map[string]any{
				"driver": "static",
				"drivers": map[string]any{
					"static": map[string]any{
						"home_provider": "/home",
						"rules": map[string]any{
							"/home": map[string]any{
								"mapping": "/home-{{substr 0 1 .Id.OpaqueId}}",
								"aliases": map[string]any{
									"/home-[0-9a-e]": map[string]any{
										"address":     "{{storage_address}}",
										"provider_id": "{{storage_id}}",
									},
									"/home-[f-z]": map[string]any{
										"address":     "{{storage2_address}}",
										"provider_id": "{{storage2_id}}",
									},
								},
							},
							"/users/[0-9a-e]": map[string]any{
								"address":       "{{storage_address}}",
								"provider_id":   "{{storage_id}}",
								"provider_path": "/users",
							},
							"{{storage_id}}": map[string]any{
								"address":     "{{storage_address}}",
								"provider_id": "{{storage_id}}",
							},
							"/users/[f-z]": map[string]any{
								"address":       "{{storage2_address}}",
								"provider_id":   "{{storage2_id}}",
								"provider_path": "/users",
							},
							"{{storage2_id}}": map[string]any{
								"address":     "{{storage2_address}}",
								"provider_id": "{{storage2_id}}",
							},
						},
					},
				},
			},
		},
	},
	"http": map[string]any{
		"address": "{{grpc_address+1}}",
		"services": map[string]any{
			"datagateway": map[string]any{
				"transfer_shared_secret": "replace-me-with-a-transfer-secret",
			},
		},
	},
}

// StorageProviderLocal is the configuration for the local storage provider
var StorageProviderLocal = map[string]any{
	"grpc": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"storageprovider": map[string]any{
				"driver": "local",
				"drivers": map[string]any{
					"local": map[string]any{
						"disable_home": "{{disable_home}}",
						"root":         "{{root}}/storage",
						"user_layout":  "{{substr 0 1 .Id.OpaqueId}}/{{.Id.OpaqueId}}",
					},
				},
			},
		},
	},
}

// GroupProviderJSON is the configuration for the json group provider
var GroupProviderJSON = map[string]any{
	"log": map[string]any{
		"level": "debug",
	},
	"grpc": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"groupprovider": map[string]any{
				"driver": "json",
				"drivers": map[string]any{
					"json": map[string]any{
						"groups": "fixtures/groups.demo.json",
					},
				},
			},
		},
	},
}

// GroupProviderLDAP is the configuration for the ldap group provider
var GroupProviderLDAP = map[string]any{
	"log": map[string]any{
		"level": "debug",
	},
	"grpc": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"groupprovider": map[string]any{
				"driver": "ldap",
				"drivers": map[string]any{
					"ldap": map[string]any{
						"uri":               "ldaps://openldap:636",
						"insecure":          true,
						"user_base_dn":      "ou=users,dc=example,dc=org",
						"group_base_dn":     "ou=groups,dc=example,dc=org",
						"user_filter":       "",
						"user_objectclass":  "posixAccount",
						"group_filter":      "",
						"group_objectclass": "groupOfNames",
						"bind_username":     "cn=admin,dc=example,dc=org",
						"bind_password":     "admin",
						"idp":               "http://localhost:20080",
						"user_schema": map[string]any{
							"id":          "openclouduuid",
							"displayName": "displayName",
							"userName":    "cn",
							"gid":         "cn",
						},
						"group_schema": map[string]any{
							"id":          "openclouduuid",
							"displayName": "description",
							"groupName":   "cn",
							"member":      "member",
						},
					},
				},
			},
		},
	},
}

// StorageProviderMultitenant is the configuration for the multitenant storage provider
var StorageProviderMultitenant = map[string]any{
	"grpc": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"storageprovider": map[string]any{
				"driver": "decomposed",
				"drivers": map[string]any{
					"decomposed": map[string]any{
						"root":                 "{{root}}/storage",
						"treetime_accounting":  true,
						"treesize_accounting":  true,
						"permissionssvc":       "{{permissions_address}}",
						"multi_tenant_enabled": true,
						"filemetadatacache": map[string]any{
							"cache_store": "noop",
						},
					},
				},
			},
		},
	},
	"shared": map[string]any{
		"multi_tenant_enabled": true,
	},
}

// OCMServerCernboxGRPC is the configuration for the Cernbox OCM gRPC server
var OCMServerCernboxGRPC = map[string]any{
	"log": map[string]any{
		"mode": "json",
	},
	"shared": map[string]any{
		"gatewaysvc": "{{grpc_address}}",
	},
	"grpc": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"gateway": map[string]any{
				"authregistrysvc":          "{{grpc_address}}",
				"userprovidersvc":          "{{grpc_address}}",
				"ocminvitemanagersvc":      "{{grpc_address}}",
				"ocmproviderauthorizersvc": "{{grpc_address}}",
			},
			"authregistry": map[string]any{
				"driver": "static",
				"drivers": map[string]any{
					"static": map[string]any{
						"rules": map[string]any{
							"basic": "{{grpc_address}}",
						},
					},
				},
			},
			"ocminvitemanager": map[string]any{
				"driver":          "{{ocm_driver}}",
				"provider_domain": "cernbox.cern.ch",
				"drivers": map[string]any{
					"json": map[string]any{
						"file": "{{invite_token_file}}",
					},
					"sql": map[string]any{
						"db_username": "{{db_username}}",
						"db_password": "{{db_password}}",
						"db_address":  "{{db_address}}",
						"db_name":     "{{db_name}}",
					},
				},
			},
			"ocmproviderauthorizer": map[string]any{
				"driver": "json",
				"drivers": map[string]any{
					"json": map[string]any{
						"providers": "{{file_providers}}",
					},
				},
			},
			"authprovider": map[string]any{
				"auth_manager": "json",
				"auth_managers": map[string]any{
					"json": map[string]any{
						"users": "fixtures/ocm-users.demo.json",
					},
				},
			},
			"userprovider": map[string]any{
				"driver": "json",
				"drivers": map[string]any{
					"json": map[string]any{
						"users": "fixtures/ocm-users.demo.json",
					},
				},
			},
		},
	},
}

// OCMServerCernboxHTTP is the configuration for the Cernbox OCM HTTP server
var OCMServerCernboxHTTP = map[string]any{
	"log": map[string]any{
		"mode": "json",
	},
	"shared": map[string]any{
		"gatewaysvc": "{{cernboxgw_address}}",
	},
	"http": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"ocmd": map[string]any{},
			"sciencemesh": map[string]any{
				"provider_domain":    "{{cernboxhttp_address}}",
				"mesh_directory_url": "http://meshdir",
				"smtp_credentials":   map[string]any{},
			},
		},
		"middlewares": map[string]any{
			"cors": map[string]any{},
			"providerauthorizer": map[string]any{
				"driver": "json",
				"drivers": map[string]any{
					"json": map[string]any{
						"providers": "fixtures/ocm-providers.demo.json",
					},
				},
			},
		},
	},
}

// OCMServerCesnetGRPC is the configuration for the Cesnet OCM gRPC server
var OCMServerCesnetGRPC = map[string]any{
	"log": map[string]any{
		"mode": "json",
	},
	"shared": map[string]any{
		"gatewaysvc": "{{grpc_address}}",
	},
	"grpc": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"gateway": map[string]any{
				"authregistrysvc":          "{{grpc_address}}",
				"userprovidersvc":          "{{grpc_address}}",
				"ocminvitemanagersvc":      "{{grpc_address}}",
				"ocmproviderauthorizersvc": "{{grpc_address}}",
			},
			"authregistry": map[string]any{
				"driver": "static",
				"drivers": map[string]any{
					"static": map[string]any{
						"rules": map[string]any{
							"basic": "{{grpc_address}}",
						},
					},
				},
			},
			"ocminvitemanager": map[string]any{
				"driver":          "{{ocm_driver}}",
				"provider_domain": "cesnet.cz",
				"drivers": map[string]any{
					"json": map[string]any{
						"file": "{{invite_token_file}}",
					},
					"sql": map[string]any{
						"db_username": "{{db_username}}",
						"db_password": "{{db_password}}",
						"db_address":  "{{db_address}}",
						"db_name":     "{{db_name}}",
					},
				},
			},
			"ocmproviderauthorizer": map[string]any{
				"driver": "json",
				"drivers": map[string]any{
					"json": map[string]any{
						"providers": "{{file_providers}}",
					},
				},
			},
			"authprovider": map[string]any{
				"auth_manager": "json",
				"auth_managers": map[string]any{
					"json": map[string]any{
						"users": "fixtures/ocm-users.demo.json",
					},
				},
			},
			"userprovider": map[string]any{
				"driver": "json",
				"drivers": map[string]any{
					"json": map[string]any{
						"users": "fixtures/ocm-users.demo.json",
					},
				},
			},
		},
	},
}

// OCMServerCesnetHTTP is the configuration for the Cesnet OCM HTTP server
var OCMServerCesnetHTTP = map[string]any{
	"log": map[string]any{
		"mode": "json",
	},
	"shared": map[string]any{
		"gatewaysvc": "{{cesnetgw_address}}",
	},
	"http": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"ocmd": map[string]any{},
			"sciencemesh": map[string]any{
				"provider_domain":    "{{cesnethttp_address}}",
				"mesh_directory_url": "http://meshdir",
				"smtp_credentials":   map[string]any{},
			},
		},
		"middlewares": map[string]any{
			"cors": map[string]any{},
			"providerauthorizer": map[string]any{
				"driver": "json",
				"drivers": map[string]any{
					"json": map[string]any{
						"providers": "fixtures/ocm-providers.demo.json",
					},
				},
			},
		},
	},
}

// OCMShareServerCernboxGRPC is the configuration for the Cernbox OCM Share gRPC server
var OCMShareServerCernboxGRPC = map[string]any{
	"log": map[string]any{
		"mode": "json",
	},
	"shared": map[string]any{
		"gatewaysvc": "{{grpc_address}}",
	},
	"grpc": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"gateway": map[string]any{
				"authregistrysvc":          "{{grpc_address}}",
				"userprovidersvc":          "{{grpc_address}}",
				"ocminvitemanagersvc":      "{{grpc_address}}",
				"ocmproviderauthorizersvc": "{{grpc_address}}",
				"storageregistrysvc":       "{{grpc_address}}",
				"ocmshareprovidersvc":      "{{grpc_address}}",
				"ocmcoresvc":               "{{grpc_address}}",
				"datagateway":              "http://{{cernboxhttp_address}}/datagateway",
				"permissionssvc":           "{{permissions_address}}",
			},
			"storageregistry": map[string]any{
				"driver": "spaces",
				"drivers": map[string]any{
					"spaces": map[string]any{
						"home_template": "/users/{{.Id.OpaqueId}}",
						"providers": map[string]any{
							"{{grpc_address}}": map[string]any{
								"spaces": map[string]any{
									"personal": map[string]any{
										"mount_point":   "/users",
										"path_template": "/users/{{.Space.Owner.Id.OpaqueId}}",
									},
								},
							},
							"{{cernboxpublicstorage_address}}": map[string]any{
								"spaces": map[string]any{
									"mountpoint": map[string]any{
										"mount_point":   "/public",
										"path_template": "/public/{{.Space.Root.OpaqueId}}",
									},
									"grant": map[string]any{
										"mount_point": ".",
									},
								},
							},
						},
					},
				},
			},
			"storageprovider": map[string]any{
				"driver":          "decomposed",
				"mount_path":      "/home",
				"mount_id":        "123e4567-e89b-12d3-a456-426655440000",
				"data_server_url": "http://{{cernboxhttp_address}}/data",
				"drivers": map[string]any{
					"decomposed": map[string]any{
						"root":                "{{root}}/storage",
						"treetime_accounting": true,
						"treesize_accounting": true,
						"permissionssvc":      "{{permissions_address}}",
						"filemetadatacache": map[string]any{
							"cache_store": "noop",
						},
					},
				},
			},
			"authregistry": map[string]any{
				"driver": "static",
				"drivers": map[string]any{
					"static": map[string]any{
						"rules": map[string]any{
							"basic":     "{{grpc_address}}",
							"ocmshares": "{{cernboxocmsharesauth_address}}",
							"machine":   "{{cernboxmachineauth_address}}",
						},
					},
				},
			},
			"ocminvitemanager": map[string]any{
				"driver":          "json",
				"provider_domain": "cernbox.cern.ch",
				"drivers": map[string]any{
					"json": map[string]any{
						"file": "{{invite_token_file}}",
					},
				},
			},
			"ocmproviderauthorizer": map[string]any{
				"driver": "json",
				"drivers": map[string]any{
					"json": map[string]any{
						"providers": "{{file_providers}}",
					},
				},
			},
			"ocmshareprovider": map[string]any{
				"driver":          "json",
				"webdav_endpoint": "http://{{cernboxwebdav_address}}",
				"provider_domain": "cernbox.cern.ch",
				"drivers": map[string]any{
					"json": map[string]any{
						"file": "{{ocm_share_cernbox_file}}",
					},
				},
			},
			"authprovider": map[string]any{
				"auth_manager": "json",
				"auth_managers": map[string]any{
					"json": map[string]any{
						"users": "fixtures/ocm-share/ocm-users.demo.json",
					},
				},
			},
			"userprovider": map[string]any{
				"driver": "json",
				"drivers": map[string]any{
					"json": map[string]any{
						"users": "fixtures/ocm-share/ocm-users.demo.json",
					},
				},
			},
		},
	},
}

// AuthMachine is the configuration for the machine auth provider
var AuthMachine = map[string]any{
	"log": map[string]any{
		"mode": "json",
	},
	"shared": map[string]any{
		"gatewaysvc": "{{cernboxgw_address}}",
	},
	"grpc": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"authprovider": map[string]any{
				"auth_manager": "machine",
				"auth_managers": map[string]any{
					"machine": map[string]any{
						"api_key":      "secret",
						"gateway_addr": "{{cernboxgw_address}}",
					},
				},
			},
		},
	},
}

// StorageProviderPublic is the configuration for the public storage provider
var StorageProviderPublic = map[string]any{
	"log": map[string]any{
		"mode": "json",
	},
	"shared": map[string]any{
		"gatewaysvc": "{{cernboxgw_address}}",
	},
	"grpc": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"publicstorageprovider": map[string]any{
				"mount_id":             "{{id}}",
				"gateway_addr":         "{{cernboxgw_address}}",
				"machine_auth_api_key": "secret",
			},
			"authprovider": map[string]any{
				"auth_manager": "ocmshares",
				"auth_managers": map[string]any{
					"ocmshares": map[string]any{
						"gatewaysvc": "{{cernboxgw_address}}",
					},
				},
			},
		},
	},
}

// WebDAVServerCernbox is the configuration for the Cernbox WebDAV server
var WebDAVServerCernbox = map[string]any{
	"log": map[string]any{
		"mode": "json",
	},
	"shared": map[string]any{
		"gatewaysvc": "{{cernboxgw_address}}",
	},
	"http": map[string]any{
		"address": "{{grpc_address}}",
		"middlewares": map[string]any{
			"auth": map[string]any{
				"token_strategy": "bearer",
			},
		},
		"services": map[string]any{
			"ocdav": map[string]any{
				"ocm_namespace":             "/public",
				"url_signing_shared_secret": "change-me-please",
			},
		},
	},
}

// AuthOCMShares is the configuration for the ocm shares auth provider
var AuthOCMShares = map[string]any{
	"log": map[string]any{
		"mode": "json",
	},
	"shared": map[string]any{
		"gatewaysvc": "{{cernboxgw_address}}",
	},
	"grpc": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"authprovider": map[string]any{
				"auth_manager": "ocmshares",
				"auth_managers": map[string]any{
					"ocmshares": map[string]any{},
				},
			},
		},
	},
}

// OCMShareServerCernboxHTTP is the configuration for the Cernbox OCM Share HTTP server
var OCMShareServerCernboxHTTP = map[string]any{
	"log": map[string]any{
		"mode": "json",
	},
	"shared": map[string]any{
		"gatewaysvc": "{{cernboxgw_address}}",
	},
	"http": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"ocmd": map[string]any{},
			"sciencemesh": map[string]any{
				"provider_domain":    "{{cernboxhttp_address}}",
				"mesh_directory_url": "http://meshdir",
				"smtp_credentials":   map[string]any{},
			},
			"datagateway": map[string]any{},
			"dataprovider": map[string]any{
				"driver": "decomposed",
				"drivers": map[string]any{
					"decomposed": map[string]any{
						"root":                "{{cernboxgw_root}}/storage",
						"treetime_accounting": true,
						"treesize_accounting": true,
						"permissionssvc":      "{{permissions_address}}",
						"filemetadatacache": map[string]any{
							"cache_store": "noop",
						},
					},
				},
			},
			"ocdav": map[string]any{
				"url_signing_shared_secret": "change-me-please",
			},
		},
		"middlewares": map[string]any{
			"cors": map[string]any{},
			"providerauthorizer": map[string]any{
				"driver": "json",
				"drivers": map[string]any{
					"json": map[string]any{
						"providers": "fixtures/ocm-providers.demo.json",
					},
				},
			},
		},
	},
}

// OCMShareServerCesnetGRPC is the configuration for the Cesnet OCM Share gRPC server
var OCMShareServerCesnetGRPC = map[string]any{
	"log": map[string]any{
		"mode": "json",
	},
	"shared": map[string]any{
		"gatewaysvc": "{{grpc_address}}",
	},
	"grpc": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"gateway": map[string]any{
				"authregistrysvc":          "{{grpc_address}}",
				"userprovidersvc":          "{{grpc_address}}",
				"ocminvitemanagersvc":      "{{grpc_address}}",
				"ocmproviderauthorizersvc": "{{grpc_address}}",
				"ocmshareprovidersvc":      "{{grpc_address}}",
				"ocmcoresvc":               "{{grpc_address}}",
				"datagateway":              "http://{{cesnethttp_address}}/datagateway",
			},
			"authregistry": map[string]any{
				"driver": "static",
				"drivers": map[string]any{
					"static": map[string]any{
						"rules": map[string]any{
							"basic": "{{grpc_address}}",
						},
					},
				},
			},
			"ocminvitemanager": map[string]any{
				"driver":          "json",
				"provider_domain": "cesnet.cz",
				"drivers": map[string]any{
					"json": map[string]any{
						"file": "{{invite_token_file}}",
					},
				},
			},
			"ocmproviderauthorizer": map[string]any{
				"driver": "json",
				"drivers": map[string]any{
					"json": map[string]any{
						"providers": "{{file_providers}}",
					},
				},
			},
			"ocmshareprovider": map[string]any{
				"driver":          "json",
				"webdav_endpoint": "http://{{cesnethttp_address}}",
				"provider_domain": "cesnet.cz",
				"drivers": map[string]any{
					"json": map[string]any{
						"file": "{{ocm_share_cesnet_file}}",
					},
				},
			},
			"ocmcore": map[string]any{
				"driver": "json",
				"drivers": map[string]any{
					"json": map[string]any{
						"file": "{{ocm_share_cesnet_file}}",
					},
				},
			},
			"authprovider": map[string]any{
				"auth_manager": "json",
				"auth_managers": map[string]any{
					"json": map[string]any{
						"users": "fixtures/ocm-users.demo.json",
					},
				},
			},
			"userprovider": map[string]any{
				"driver": "json",
				"drivers": map[string]any{
					"json": map[string]any{
						"users": "fixtures/ocm-users.demo.json",
					},
				},
			},
			"storageregistry": map[string]any{
				"driver": "spaces",
				"drivers": map[string]any{
					"spaces": map[string]any{
						"providers": map[string]any{
							"{{grpc_address}}": map[string]any{
								"spaces": map[string]any{
									"mountpoint": map[string]any{
										"mount_point":   "/ocm",
										"path_template": "/ocm/{{.Space.Root.OpaqueId}}",
									},
									"grant": map[string]any{
										"mount_point": ".",
									},
								},
							},
						},
					},
				},
			},
			"storageprovider": map[string]any{
				"driver":          "ocmreceived",
				"mount_path":      "/ocm",
				"mount_id":        "984e7351-2729-4417-99b4-ab5e6d41fa97",
				"data_server_url": "http://{{cesnethttp_address}}/data",
				"drivers": map[string]any{
					"ocmreceived": map[string]any{},
				},
			},
		},
	},
}

// OCMShareServerCesnetHTTP is the configuration for the Cesnet OCM Share HTTP server
var OCMShareServerCesnetHTTP = map[string]any{
	"log": map[string]any{
		"mode": "json",
	},
	"shared": map[string]any{
		"gatewaysvc": "{{cesnetgw_address}}",
	},
	"http": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"ocmd": map[string]any{},
			"sciencemesh": map[string]any{
				"provider_domain":    "{{cesnethttp_address}}",
				"mesh_directory_url": "http://meshdir",
				"smtp_credentials":   map[string]any{},
			},
			"datagateway": map[string]any{},
			"dataprovider": map[string]any{
				"driver": "ocmreceived",
				"drivers": map[string]any{
					"ocmreceived": map[string]any{},
				},
			},
		},
		"middlewares": map[string]any{
			"cors": map[string]any{},
			"providerauthorizer": map[string]any{
				"driver": "json",
				"drivers": map[string]any{
					"json": map[string]any{
						"providers": "fixtures/ocm-providers.demo.json",
					},
				},
			},
		},
	},
}

// StorageProviderNextcloud is the configuration for the nextcloud storage provider
var StorageProviderNextcloud = map[string]any{
	"grpc": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"storageprovider": map[string]any{
				"driver": "nextcloud",
				"drivers": map[string]any{
					"nextcloud": map[string]any{
						"endpoint":  "http://localhost:8080/apps/sciencemesh/",
						"mock_http": true,
					},
				},
			},
		},
	},
}

// UserProviderDemo is the configuration for the demo user provider
var UserProviderDemo = map[string]any{
	"grpc": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"userprovider": map[string]any{
				"driver": "demo",
			},
		},
	},
}

// UserProviderLDAP is the configuration for the ldap user provider
var UserProviderLDAP = map[string]any{
	"log": map[string]any{
		"level": "debug",
	},
	"grpc": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"userprovider": map[string]any{
				"driver": "ldap",
				"drivers": map[string]any{
					"ldap": map[string]any{
						"uri":               "ldaps://openldap:636",
						"insecure":          true,
						"user_base_dn":      "ou=users,dc=example,dc=org",
						"group_base_dn":     "ou=groups,dc=example,dc=org",
						"user_filter":       "",
						"user_objectclass":  "posixAccount",
						"group_filter":      "",
						"group_objectclass": "groupOfNames",
						"bind_username":     "cn=admin,dc=example,dc=org",
						"bind_password":     "admin",
						"idp":               "http://localhost:20080",
						"user_schema": map[string]any{
							"id":          "openclouduuid",
							"displayName": "displayName",
							"userName":    "cn",
						},
						"group_schema": map[string]any{
							"id":          "cn",
							"displayName": "cn",
							"groupName":   "cn",
							"member":      "member",
						},
					},
				},
			},
		},
	},
}

// GatewaySharded is the configuration for the sharded gateway
var GatewaySharded = map[string]any{
	"shared": map[string]any{
		"jwt_secret": "changemeplease",
		"gatewaysvc": "{{grpc_address}}",
	},
	"grpc": map[string]any{
		"address": "{{grpc_address}}",
		"services": map[string]any{
			"gateway": map[string]any{
				"storageregistrysvc": "{{grpc_address}}",
				"stat_cache_ttl":     1,
				"permissionssvc":     "{{permissions_address}}",
			},
			"authregistry": map[string]any{
				"driver": "static",
				"drivers": map[string]any{
					"static": map[string]any{
						"rules": map[string]any{
							"basic": "{{users_address}}",
						},
					},
				},
			},
			"storageregistry": map[string]any{
				"driver": "spaces",
				"drivers": map[string]any{
					"spaces": map[string]any{
						"home_template": "/users/{{.Id.OpaqueId}}",
						"providers": map[string]any{
							"{{storage_address}}": map[string]any{
								"spaces": map[string]any{
									"project": map[string]any{
										"mount_point":   "/projects/[a-k]",
										"path_template": "/projects/{{.Space.Name}}",
									},
								},
							},
							"{{storage2_address}}": map[string]any{
								"spaces": map[string]any{
									"project": map[string]any{
										"mount_point":   "/projects/[l-z]",
										"path_template": "/projects/{{.Space.Name}}",
									},
								},
							},
							"{{homestorage_address}}": map[string]any{
								"spaces": map[string]any{
									"personal": map[string]any{
										"mount_point":   "/users",
										"path_template": "/users/{{.Space.Owner.Id.OpaqueId}}",
									},
								},
							},
						},
					},
				},
			},
		},
	},
	"http": map[string]any{
		"address": "{{grpc_address+1}}",
		"services": map[string]any{
			"datagateway": map[string]any{
				"transfer_shared_secret": "replace-me-with-a-transfer-secret",
			},
		},
	},
}
