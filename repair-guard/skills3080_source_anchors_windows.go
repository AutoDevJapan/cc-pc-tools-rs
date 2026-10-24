package main

// Fixed identities of the retained original public collection, independently
// recorded before this operation manifest. No caller-supplied replacement
// catalog, index, tree or baseline is accepted. Only selected saved objects are
// read; absence never triggers acquisition or blanket license acceptance.
var skillsSourceAnchors = map[string]skillsSourceAnchor{
	"6mile/skills":                                  {"799bd907887120d83b82e64fbf37094c653a5144b764fef0b70dd4d5e2219607", "11e79e2b3e1503ec231ede8e20298f98e7734715a304835f37b3f68bee0b7b5b", "e9c2d520d36b28610f93d355605c7449c621f8c4d5df52e3f0f07dbfe2b06bfd", true},
	"cloudflare/security-audit-skill":               {"68574f1644d934e326ad9c04fa24192e0fbe98e4a45bb7286ddcdc4f519d5822", "c37547841bcb1429eb2243535f9d4438b1bd4f9b387d00d2476469869690d3bc", "b12cb0e2f1a6afc495d40e3581a55e0036e2c278e32f06527e7a058fdc9b5fb5", false},
	"docker/skills":                                 {"539db850a2139015a4f3f1ccf8f4fafd202c125503168dcb18ca235aa1c43281", "6115d5d55210f98dc134f187e859a82707de70062d0a2683bc96046af9d1c396", "9ad4a041edd05bca4ae1f19be95db855b7c1777ff236093db76f660cda89f9d8", false},
	"hirokaji/jp-ui-contracts":                      {"966a953973e600ff12740bd39683ed5450e8970b9f8536941c1c07404f986edd", "2518622fefa2d17c008ab6bad02be1f8fee03d48be9f96161bc5eb99f812e569", "3a17fd087fd763b50e7fffb208c3af514ac555d884ff5512600f1657964ecc0d", false},
	"kepano/obsidian-skills":                        {"ac3242d17a8edc973c68585167d40590f110ddc63b06149d7eab870df0c95c88", "d69138c9316a49bfd78b2c9742a4b869ce9aa3c9c6907a1a1f27b41af594734c", "d5ad0e1381b14e739ebb6dfe02d43f3ad3c74fe7ca3676af26d9e335eb741b8f", false},
	"mattpocock/skills":                             {"d813b3fc5097b469b471941cd7a82e31edc0651853db6106403b116532ec7735", "0f1fe734ad6a9470cf31b32349140c2e284d5a866d7e7d043fe5445e51a6cf3d", "503898356c72b60096ee955f49f238da22909f4f6cb1a706736ff620493916f7", false},
	"MengTo/Skills":                                 {"1a9fa91b6ac4e9156b2f3548ca6cf3cdcb4377c6c52cc1bfb315fcb3efb90643", "78e159cb1fabd9072bf8a6c667fe55aed8d1525cfffb2a5d2b94932e1faefc59", "56f6dde2851c2f35f7deb6c19df65dae34dadba16b0363beda82ac202016e17e", false},
	"mizchi/skills":                                 {"73c6b4dbc0f2056b5bdf13db6aae2426b1e70146f4554df792b4ab64d3174076", "e8876d7e3f6f9e91d853508fe634f274c3e02c83ec93c23a18df65208730a859", "5fe98da85ac5ede99a13e66aa72869463490bf722bbc7f69b4007fbdf15b62a8", false},
	"nateherkai/AIS-OS":                             {"b83292008f5e1547a0e2415a4a23c44c5ee4b22d82505b3458304f45493c882e", "e9edb76c606c32861ffac45bdf611aa65a10fcb125c740c4048135e0651b16e8", "1ba70fb1844fa269488dbdfb7089feec7ac039e4707686236ca75c1127b37c5a", false},
	"Nutlope/hallmark":                              {"7c8daf61ec4d327de22c02f6b114e7c681ea150b63b950883c8b11c2db28442d", "3eacac04a890fcecb81dfdd5a9fa6d82aeeb5b1cf556cf9c8dba7fa0e06da758", "10a2516760b0d69ee5795b0d2ab2330122cbba3721c71e4e755433eac702f2cf", false},
	"obra/superpowers":                              {"d326672c32b0b5cf91b98e6c41cf5361b029818dfcc0bf5931a48f3667d7b117", "3e4ab92b1b4a2baa997373ad1380d9eead3b310b1a280d79630e63c87d5286ea", "d9adf46b7f6b6ca603a7a8ea7feeddf4daa5972d4d3a2bd91be5104e6703a101", false},
	"openai/skills":                                 {"abeb560761fd385a1b199f8234aa8f69c1e90556398021e9e0d51bb9430d50be", "54418bcb6cf4d968db090aa41711b11bcb7e3f8752318b8491e12a824227f75a", "7e0b01fefbe04ccf36a05b40f3b0ee24d2d88756f766894de510263d4728e1f4", false},
	"Tencent/teamai-cli":                            {"ac363354bcfad1471439ad9df9479004684c9c4a6f9275e55fb3951c589eb68b", "1b91ad16817aa9d588f339c432160f9b4b4b13419435e715b91c665c56ee9131", "bbb9dcfa2c179520997ba275da012fd38445eded1833715d28b3a713bf5acc6b", false},
	"VoltAgent/awesome-design-md":                   {"505a223d71742e322688e0c14ad0f10ddd3f15cd3fee1b443ffe5fc091f6782e", "18d4012d7c5ea4ba2117f4d9dfcd25646d1a6bc6df1eefbd5a73551bba5bbd6e", "4fc71af66ecb5f70a736e32c920f2983461e806e42598175fc5c78062ef7fad1", false},
	"x1xhlol/system-prompts-and-models-of-ai-tools": {"477ac8c5b178c4c781bf5baf0b26c6a316ed0ae6e9001ad28f0ac0ab9844b54f", "416b91db26db0b0c4447d46bbb945259d89ac393e45ee76b5ebceb09cde459f5", "674dc969c4d800fefbf65a7ecb1c875e919118aa48fd0a2133ede51385f90939", false},
}
