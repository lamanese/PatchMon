import { Check, Copy, Terminal } from "lucide-react";
import { useState } from "react";

const copyToClipboard = async (text) => {
	try {
		if (navigator.clipboard && window.isSecureContext) {
			await navigator.clipboard.writeText(text);
			return true;
		}
		const textArea = document.createElement("textarea");
		textArea.value = text;
		textArea.style.position = "fixed";
		textArea.style.left = "-999999px";
		document.body.appendChild(textArea);
		textArea.focus();
		textArea.select();
		const ok = document.execCommand("copy");
		document.body.removeChild(textArea);
		return ok;
	} catch {
		return false;
	}
};

// extractHostCommands pulls the commands out of the agent's "[PatchMon]" help
// blocks: the indented lines that follow a "[PatchMon] ...:" line. A trailing
// "   # comment" explains the command and is not part of what gets copied.
export const extractHostCommands = (output) => {
	if (!output) return [];
	const lines = output.replace(/\r\n/g, "\n").replace(/\r/g, "\n").split("\n");
	const commands = [];
	const seen = new Set();
	let inBlock = false;
	for (const line of lines) {
		if (line.startsWith("[PatchMon]")) {
			inBlock = line.trimEnd().endsWith(":");
			continue;
		}
		if (inBlock && /^ {2}\S/.test(line) && !line.startsWith("  - ")) {
			const [command, comment] = line.trim().split(/\s{3,}#\s*/);
			if (command && !seen.has(command)) {
				seen.add(command);
				commands.push({ command, comment: comment || "" });
			}
			continue;
		}
		inBlock = false;
	}
	return commands;
};

export const CopyCommandButton = ({ text, label = "Copy", className = "" }) => {
	const [copied, setCopied] = useState(false);
	return (
		<button
			type="button"
			onClick={async () => {
				if (await copyToClipboard(text)) {
					setCopied(true);
					setTimeout(() => setCopied(false), 1500);
				}
			}}
			className={`inline-flex items-center gap-1 rounded border border-secondary-300 dark:border-secondary-600 bg-white dark:bg-secondary-800 px-2 py-1 text-xs font-medium text-secondary-700 dark:text-white hover:bg-secondary-50 dark:hover:bg-secondary-700 ${className}`}
			title="Copy to clipboard"
		>
			{copied ? (
				<Check className="h-3 w-3 text-green-600" />
			) : (
				<Copy className="h-3 w-3" />
			)}
			{copied ? "Copied" : label}
		</button>
	);
};

// PatchRunHelp lists the commands the agent suggested for a failed run, each
// with a copy button. They are run by an administrator on the host; PatchMon
// never runs them.
const PatchRunHelp = ({ output, className = "" }) => {
	const commands = extractHostCommands(output);
	if (commands.length === 0) return null;
	return (
		<div
			className={`rounded-lg border border-amber-300 bg-amber-50 dark:border-amber-700 dark:bg-amber-900/20 p-3 ${className}`}
		>
			<div className="flex items-center justify-between gap-2 flex-wrap">
				<div className="flex items-center gap-2 text-sm font-medium text-amber-900 dark:text-amber-100">
					<Terminal className="h-4 w-4" />
					Suggested commands for an administrator on the host
				</div>
				{commands.length > 1 && (
					<CopyCommandButton
						text={commands.map((c) => c.command).join("\n")}
						label="Copy all"
					/>
				)}
			</div>
			<ul className="mt-2 space-y-1.5">
				{commands.map((c) => (
					<li key={c.command} className="flex items-start gap-2">
						<CopyCommandButton text={c.command} className="flex-shrink-0" />
						<div className="min-w-0">
							<code className="block font-mono text-xs text-secondary-900 dark:text-white break-all">
								{c.command}
							</code>
							{c.comment && (
								<span className="text-xs text-secondary-600 dark:text-white/70">
									{c.comment}
								</span>
							)}
						</div>
					</li>
				))}
			</ul>
			<p className="mt-2 text-xs text-amber-900/80 dark:text-amber-100/80">
				PatchMon does not run these commands. The explanation is at the end of
				the output below.
			</p>
		</div>
	);
};

export default PatchRunHelp;
