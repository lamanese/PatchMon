// Fork branding. The product name is deliberately one constant so that a rename
// touches this file only. Technical identifiers (agent binary, service names,
// config paths, env prefix, webhook header) are NOT derived from it.
export const PRODUCT_NAME = "amanIT PatMan";
export const PRODUCT_NAME_SHORT = "PatMan";
export const VENDOR_NAME = "amanIT GmbH";
export const VENDOR_URL = "https://amanit.swiss";

// Public source repository of this fork (AGPL v3 section 13 offer).
export const SOURCE_REPO_URL = "https://github.com/lamanese/PatchMon";
export const SOURCE_CODE_URL = `${SOURCE_REPO_URL}/tags`;
export const DOCS_BASE_URL = `${SOURCE_REPO_URL}/blob/feat/remote-reboot/docs`;

// Tag that holds the source of a given running version (tags are v<version>).
export const sourceUrlForVersion = (version) =>
	version ? `${SOURCE_REPO_URL}/tree/v${version}` : SOURCE_CODE_URL;
