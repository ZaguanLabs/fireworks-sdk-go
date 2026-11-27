package sdk

// Immutable data pins used by the Fireworks 1.2.39 tokenizer loader.
type kimiProfile struct {
	name, repository, revision, model, configSHA, templateSHA, codeSHA string
}

const kimiVocabSHA = "b6c497a7469b33ced9c38afb1ad6e47f03f5e5dc05f15930799210ec050c5103"

var kimiProfiles = []kimiProfile{
	{"kimi_k2_thinking_v1", "moonshotai/Kimi-K2-Thinking", "612681931a8c906ddb349f8ad0f582cb552189cd", "kimi-k2-thinking", "d0a17617078b53af4ff77546d8ef190d28efad6ef2ed0ee69e4b3c97a6f829fd", "07a791b8563c5159054394745049c8e487986ecb34aaab77727e1b158bbe6a5c", ""},
	{"kimi_k2_base_v1", "moonshotai/Kimi-K2-Base", "ce72df012259dcc55d945e890f815fe7ef69159c", "kimi-k2-base", "a4cdd80882ea8c7bb1a0b8710223a2d1ca5dd994eb553679863209c6e905b112", "", ""},
	{"kimi_k2_instruct_v1", "moonshotai/Kimi-K2-Instruct", "fd1984e2b7a3350dbf7305fe73a4ede25c14de50", "kimi-k2-instruct", "5ba77d5822137ac3cb9337d2b84561d10425562cf8b8f2c74c91a832516a778d", "39e8c195b474c1a4148046f41fedd177b2ff8f420fa376e52fcdddb829e8f0c5", ""},
	{"kimi_k2_instruct_0905_v1", "moonshotai/Kimi-K2-Instruct-0905", "ac6c49f04883bd0a0598b790693a72061c676629", "kimi-k2-instruct-0905", "5ba77d5822137ac3cb9337d2b84561d10425562cf8b8f2c74c91a832516a778d", "00938c355ca7195fd4f9c923e93af063846d4d5005e06999274f8db57b65bb0b", ""},
	{"kimi_k25_v1", "moonshotai/Kimi-K2.5", "2426b45b6af0da48d0dcce71bbce6225e5c73adc", "kimi-k2p5", "12fcab43d2b6068f46769f5ff373960bf7c17a94d7abbc50e2491306b2f6cf58", "7c91e4b3578b518bfb388d2e3dc645daf0fbf843656e887db75d12891e0030ff", ""},
	{"kimi_k26_v1", "moonshotai/Kimi-K2.6", "b5aabbfb20227ed42becbf5541dbffd213942c58", "kimi-k2p6", "12fcab43d2b6068f46769f5ff373960bf7c17a94d7abbc50e2491306b2f6cf58", "8bf859698fd4781c0e1e1c63ce74422aab27e53ccc5f47116317c64cda06132f", ""},
	{"kimi_k27_code_v1", "moonshotai/Kimi-K2.7-Code", "74797c9c62378b951a1f6fcf5c4631024e9b8bef", "kimi-k2p7-code", "6dfafeacca3d44748fc4a2fcc6725be5ea616ecc5b95f5a8b4277077c06e7d9f", "8d21200dff7e85beaf51c8b110952e039ffd0779f615d64d63293d74773360f9", ""},
	{"kimi_k3", "moonshotai/Kimi-K3", "301be1b88c89c0d3a763da6301352cb8fe399e90", "kimi-k3", "5d0803c94db9cd78763499e0956c95fd5a225c14a727e5a6cf5db3f96f010a6e", "", "f28ea66e2d862a2a5814970b2ce40c2f7d8296ff09aed90a7e7def689b906944"},
	{"kimi_k31", "moonshotai/Kimi-K3.1-Pro", "d9e99b0792a5e93b78ea9bea62a68167e36ddd9e", "kimi-k3p1-pro", "9fc322181b8ec44236e148916aea4b6c2467b66ded7bfaff0efe623ad750b852", "", "204ee39fbf13a6d6608b0d3ca1b4b04a14ac5f0a062a7fdee02e61f5d936f9c6"},
}
