// Helpers for endpoints that answer with a file (PDF) as a blob.

export const filenameFromDisposition = (disposition, fallback) => {
	const m = /filename\*?=(?:utf-8'')?"?([^";]+)"?/i.exec(disposition || "");
	if (!m) return fallback;
	try {
		return decodeURIComponent(m[1]);
	} catch {
		return m[1];
	}
};

export const downloadBlob = (data, filename, type = "application/pdf") => {
	const blob = data instanceof Blob ? data : new Blob([data], { type });
	const url = window.URL.createObjectURL(blob);
	const a = document.createElement("a");
	a.href = url;
	a.download = filename;
	document.body.appendChild(a);
	a.click();
	a.remove();
	window.URL.revokeObjectURL(url);
};

// With responseType "blob" axios also delivers JSON error bodies as a Blob.
export const errorFromBlobResponse = async (err, fallback) => {
	const data = err?.response?.data;
	if (data instanceof Blob) {
		try {
			return JSON.parse(await data.text()).error || fallback;
		} catch {
			return fallback;
		}
	}
	return data?.error || fallback;
};
