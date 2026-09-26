import {
	Bell,
	Check,
	Clock,
	Globe,
	Loader2,
	Mail,
	Send,
	X,
} from "lucide-react";
import { Fragment, useState } from "react";
import { SiDiscord, SiNtfy, SiSlack } from "react-icons/si";
import { useToast } from "../../contexts/ToastContext";

const REPORT_SECTIONS = [
	{ id: "executive_summary", label: "Executive summary" },
	{ id: "compliance_summary", label: "Compliance summary" },
	{ id: "recent_patch_runs", label: "Recent patch runs" },
	{ id: "hosts_offline", label: "Hosts / status" },
	{ id: "open_alerts", label: "Open alerts" },
	{ id: "hosts_by_updates", label: "Hosts by outstanding updates" },
	{ id: "top_security_packages", label: "Top outdated security packages" },
	{ id: "host_overview", label: "Host overview" },
	{ id: "security_updates_by_host", label: "Security updates by host" },
	{ id: "disks", label: "Disks" },
	{ id: "patch_activity", label: "Patch activity (period)" },
	{ id: "reboots", label: "Reboots (period)" },
];

const REPORT_LANGUAGES = [
	{ id: "en", label: "English" },
	{ id: "de", label: "Deutsch" },
];

const REPORT_PERIODS = [7, 30, 90];

const INTERNAL_DEFAULT_SECTIONS = [
	"executive_summary",
	"compliance_summary",
	"recent_patch_runs",
];

export const CUSTOMER_DEFAULT_SECTIONS = [
	"executive_summary",
	"host_overview",
	"security_updates_by_host",
	"patch_activity",
	"reboots",
	"disks",
];

const MAX_RECIPIENTS = 10;
const RECIPIENT_RE = /^[^\s@,;<>]+@[^\s@,;<>]+\.[^\s@,;<>]+$/i;

// Adds the addresses in raw (separated by comma, semicolon or whitespace)
// to list. On any invalid address nothing is added.
const addRecipients = (list, raw) => {
	const next = [...list];
	for (const part of String(raw || "").split(/[\s,;]+/)) {
		const addr = part.trim().toLowerCase();
		if (!addr) continue;
		if (!RECIPIENT_RE.test(addr)) {
			return { list, error: `"${part.trim()}" is not a valid e-mail address` };
		}
		if (next.includes(addr)) continue;
		if (next.length >= MAX_RECIPIENTS) {
			return { list, error: `At most ${MAX_RECIPIENTS} recipients per report` };
		}
		next.push(addr);
	}
	return { list: next, error: null };
};

const languageLabel = (id) =>
	REPORT_LANGUAGES.find((l) => l.id === (id === "de" ? "de" : "en")).label;

// /hosts/admin/list delivers host_group_memberships; accept hostGroups too.
const groupIdsOfHost = (h) => {
	const groups = Array.isArray(h?.host_group_memberships)
		? h.host_group_memberships
		: Array.isArray(h?.hostGroups)
			? h.hostGroups
			: [];
	return groups.map((g) => g?.id).filter(Boolean);
};

/** Summary for ReportConfirmDialog. hostCount is null when hosts is unknown. */
export const buildReportSummary = ({
	groupIds,
	hostGroups,
	hosts,
	recipients,
	destinations,
	smtpId,
	language,
}) => {
	const ids = Array.isArray(groupIds) ? groupIds : [];
	const names = new Map(
		(Array.isArray(hostGroups) ? hostGroups : []).map((g) => [
			g.id,
			g.name || g.id,
		]),
	);
	const idSet = new Set(ids);
	const smtp = (Array.isArray(destinations) ? destinations : []).find(
		(d) => d.id === smtpId,
	);
	return {
		groups: ids.map((id) => names.get(id) || id),
		hostCount: Array.isArray(hosts)
			? hosts.filter((h) => groupIdsOfHost(h).some((id) => idSet.has(id)))
					.length
			: null,
		recipients: Array.isArray(recipients) ? recipients : [],
		sender: smtp?.display_name || "",
		language: languageLabel(language),
	};
};

export const CHANNEL_TYPES = [
	{
		value: "webhook",
		label: "Webhook",
		description: "Generic, Discord, or Slack",
		icon: Globe,
		brandIcons: { discord: SiDiscord, slack: SiSlack },
	},
	{
		value: "email",
		label: "Email",
		description: "SMTP delivery",
		icon: Mail,
	},
	{
		value: "ntfy",
		label: "ntfy",
		description: "Push notifications via ntfy.sh",
		icon: SiNtfy,
	},
	{
		value: "internal",
		label: "Internal Alerts",
		description: "Alert records in the Alerts tab",
		icon: Bell,
	},
];

const FREQUENCY_OPTIONS = [
	{ value: "daily", label: "Daily" },
	{ value: "weekdays", label: "Weekdays (Mon-Fri)" },
	{ value: "weekly", label: "Weekly" },
	{ value: "monthly", label: "Monthly" },
];

const MONTH_DAY_PRESETS = [
	{ value: "1", label: "1st" },
	{ value: "15", label: "15th" },
	{ value: "L", label: "Last day" },
];

const DAY_LABELS = [
	{ value: "1", short: "Mon" },
	{ value: "2", short: "Tue" },
	{ value: "3", short: "Wed" },
	{ value: "4", short: "Thu" },
	{ value: "5", short: "Fri" },
	{ value: "6", short: "Sat" },
	{ value: "0", short: "Sun" },
];

const buildCron = (frequency, time, days, monthDay) => {
	const [h, m] = (time || "08:00").split(":");
	const hour = Number.parseInt(h, 10) || 0;
	const minute = Number.parseInt(m, 10) || 0;
	switch (frequency) {
		case "weekdays":
			return `${minute} ${hour} * * 1-5`;
		case "weekly":
			return `${minute} ${hour} * * ${days.length > 0 ? days.join(",") : "1"}`;
		case "monthly":
			return `${minute} ${hour} ${monthDay || "1"} * *`;
		default:
			return `${minute} ${hour} * * *`;
	}
};

export const describeSchedule = (expr) => {
	if (!expr) return "";
	const parts = expr.trim().split(/\s+/);
	if (parts.length !== 5) return expr;
	const [min, hour, dom, , dow] = parts;
	const h = Number.parseInt(hour, 10);
	const m = Number.parseInt(min, 10);
	const time =
		!Number.isNaN(h) && !Number.isNaN(m)
			? `${String(h).padStart(2, "0")}:${String(m).padStart(2, "0")}`
			: null;
	if (!time) return expr;
	if (dom === "*" && dow === "*") return `Daily at ${time}`;
	if (dom === "*" && dow === "1-5") return `Weekdays at ${time}`;
	if (dom !== "*" && dow === "*") {
		if (dom === "L") return `Last day of month at ${time}`;
		const ordinal =
			dom === "1" || dom === "21" || dom === "31"
				? "st"
				: dom === "2" || dom === "22"
					? "nd"
					: dom === "3" || dom === "23"
						? "rd"
						: "th";
		return `${dom}${ordinal} of month at ${time}`;
	}
	if (dom === "*" && dow && dow !== "*") {
		const dayNames = {
			0: "Sun",
			1: "Mon",
			2: "Tue",
			3: "Wed",
			4: "Thu",
			5: "Fri",
			6: "Sat",
		};
		const days = dow
			.split(",")
			.map((d) => dayNames[d] || d)
			.join(", ");
		return `${days} at ${time}`;
	}
	return expr;
};

export const channelIcon = (type) => {
	const ct = CHANNEL_TYPES.find((c) => c.value === type);
	if (!ct) return null;
	const Icon = ct.icon;
	return <Icon className="h-4 w-4" />;
};

export const INPUT =
	"w-full px-3 py-2 bg-white dark:bg-secondary-900 border border-secondary-300 dark:border-secondary-600 rounded-md text-sm text-secondary-900 dark:text-white focus:ring-2 focus:ring-primary-500 focus:border-primary-500 placeholder-secondary-400";
export const SELECT = `${INPUT} appearance-none`;

/* ───────────────── Report Modal ───────────────── */

export const ReportConfirmDialog = ({
	open,
	title,
	summary,
	confirmLabel,
	onConfirm,
	onCancel,
}) => {
	if (!open || !summary) return null;
	const rows = [
		[
			"Host groups",
			summary.groups.length > 0 ? summary.groups.join(", ") : "none",
		],
		[
			"Hosts",
			summary.hostCount == null ? "unknown" : String(summary.hostCount),
		],
		[
			"Recipients",
			summary.recipients.length > 0 ? summary.recipients.join(", ") : "none",
		],
		["Sender (SMTP account)", summary.sender || "unknown"],
		["Language", summary.language],
	];
	return (
		<div
			className="fixed inset-0 bg-black/50 flex items-center justify-center z-[60]"
			onClick={onCancel}
		>
			<div
				role="dialog"
				aria-modal="true"
				className="bg-white dark:bg-secondary-800 rounded-lg shadow-xl max-w-md w-full mx-4"
				onClick={(e) => e.stopPropagation()}
			>
				<div className="px-6 py-4 border-b border-secondary-200 dark:border-secondary-600">
					<h3 className="text-lg font-semibold text-secondary-900 dark:text-white">
						{title}
					</h3>
				</div>
				<div className="px-6 py-4 space-y-3">
					<p className="text-sm text-secondary-600 dark:text-secondary-300">
						This report goes to external recipients. Check the scope before you
						continue.
					</p>
					<dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
						{rows.map(([k, v]) => (
							<Fragment key={k}>
								<dt className="text-secondary-500 dark:text-secondary-400">
									{k}
								</dt>
								<dd className="text-secondary-900 dark:text-white break-all">
									{v}
								</dd>
							</Fragment>
						))}
					</dl>
					{summary.hostCount === 0 && (
						<p className="text-xs text-amber-700 dark:text-amber-300">
							The selected host groups contain no hosts right now.
						</p>
					)}
				</div>
				<div className="px-6 py-4 border-t border-secondary-200 dark:border-secondary-600 flex justify-end gap-2">
					<button type="button" className="btn-outline" onClick={onCancel}>
						Cancel
					</button>
					<button
						type="button"
						className="btn-primary flex items-center gap-1"
						onClick={onConfirm}
					>
						<Send className="h-4 w-4" />
						{confirmLabel}
					</button>
				</div>
			</div>
		</div>
	);
};

const ReportModal = ({
	isOpen,
	onClose,
	onSave,
	editingReport,
	destinations,
	hostGroups,
	hosts,
	isPending,
	duplicate,
}) => {
	const isDuplicate = Boolean(duplicate && editingReport);
	// A duplicate always starts as a disabled internal report: recipients
	// are never copied from one customer to the next.
	const sourceIsCustomer = Array.isArray(editingReport?.email_recipients);
	const initialMode =
		!isDuplicate && sourceIsCustomer ? "customer" : "internal";
	const defRow = editingReport?.definition || {};

	// Parse existing cron on init
	const parseCronInit = () => {
		let frequency = "daily";
		let time = "08:00";
		let days = ["1"];
		let monthDay = "1";
		if (editingReport?.cron_expr) {
			const parts = editingReport.cron_expr.trim().split(/\s+/);
			if (parts.length === 5) {
				const [min, hour, dom, , dow] = parts;
				const h = Number.parseInt(hour, 10);
				const m = Number.parseInt(min, 10);
				if (!Number.isNaN(h) && !Number.isNaN(m)) {
					time = `${String(h).padStart(2, "0")}:${String(m).padStart(2, "0")}`;
				}
				if (dow === "1-5") frequency = "weekdays";
				else if (dom !== "*") {
					frequency = "monthly";
					monthDay = dom;
				} else if (dow && dow !== "*") {
					frequency = "weekly";
					days = dow.split(",");
				}
			}
		}
		return { frequency, time, days, monthDay };
	};
	const cronInit = parseCronInit();

	const [form, setForm] = useState({
		mode: initialMode,
		name: isDuplicate
			? `${editingReport.name || ""} (copy)`
			: editingReport?.name || "",
		frequency: cronInit.frequency,
		time: cronInit.time,
		days: cronInit.days,
		monthDay: cronInit.monthDay,
		enabled: isDuplicate ? false : editingReport?.enabled !== false,
		// The single destination of a customer report is its SMTP account,
		// not an internal destination.
		destination_ids:
			Array.isArray(editingReport?.destination_ids) && !sourceIsCustomer
				? editingReport.destination_ids
				: [],
		email_recipients:
			initialMode === "customer" ? [...editingReport.email_recipients] : [],
		smtp_destination_id:
			sourceIsCustomer && Array.isArray(editingReport?.destination_ids)
				? editingReport.destination_ids[0] || ""
				: "",
		sections:
			Array.isArray(defRow.sections) && defRow.sections.length > 0
				? defRow.sections
				: INTERNAL_DEFAULT_SECTIONS,
		host_group_ids: Array.isArray(defRow.host_group_ids)
			? defRow.host_group_ids
			: [],
		top_hosts: defRow.limits?.top_hosts ?? 20,
		language: defRow.language === "de" ? "de" : "en",
		period_days: REPORT_PERIODS.includes(Number(defRow.period_days))
			? Number(defRow.period_days)
			: 30,
	});
	const [recipientInput, setRecipientInput] = useState("");
	const [confirmBody, setConfirmBody] = useState(null);
	const toast = useToast();

	if (!isOpen) return null;

	const isCustomer = form.mode === "customer";
	const emailAccounts = destinations.filter(
		(d) => d.channel_type === "email" && d.enabled,
	);
	const smtpValid = emailAccounts.some(
		(d) => d.id === form.smtp_destination_id,
	);
	// Internal destinations never receive reports; stored ids of deleted
	// destinations are dropped on save.
	const reportDestinations = destinations.filter(
		(d) => d.channel_type !== "internal",
	);
	const reportDestinationIds = new Set(reportDestinations.map((d) => d.id));

	const upd = (key, value) => setForm((p) => ({ ...p, [key]: value }));
	const toggleArr = (key, id) =>
		setForm((p) => ({
			...p,
			[key]: p[key].includes(id)
				? p[key].filter((x) => x !== id)
				: [...p[key], id],
		}));

	const toggleDay = (d) =>
		setForm((p) => ({
			...p,
			days: p.days.includes(d) ? p.days.filter((x) => x !== d) : [...p.days, d],
		}));

	const switchMode = (mode) =>
		setForm((p) => {
			if (p.mode === mode) return p;
			const next = { ...p, mode };
			if (mode === "customer") {
				const selected = new Set(p.sections);
				if (
					p.sections.length === INTERNAL_DEFAULT_SECTIONS.length &&
					INTERNAL_DEFAULT_SECTIONS.every((x) => selected.has(x))
				) {
					next.sections = [...CUSTOMER_DEFAULT_SECTIONS];
				}
				if (!p.smtp_destination_id && emailAccounts.length === 1) {
					next.smtp_destination_id = emailAccounts[0].id;
				}
			}
			return next;
		});

	// silent (blur): an invalid address stays in the field without a toast;
	// Enter/comma and Save report it.
	const commitRecipientInput = ({ silent = false } = {}) => {
		if (!recipientInput.trim()) return;
		const { list, error } = addRecipients(
			form.email_recipients,
			recipientInput,
		);
		if (error) {
			if (!silent) toast.warning(error);
			return;
		}
		upd("email_recipients", list);
		setRecipientInput("");
	};

	const removeRecipient = (addr) =>
		setForm((p) => ({
			...p,
			email_recipients: p.email_recipients.filter((x) => x !== addr),
		}));

	const handleSave = () => {
		if (!form.name.trim()) {
			toast.warning("Report name is required");
			return;
		}
		if (form.frequency === "weekly" && form.days.length === 0) {
			toast.warning("Select at least one day");
			return;
		}
		const cronExpr = buildCron(
			form.frequency,
			form.time,
			form.days,
			form.monthDay,
		);
		// Drop ids of groups that no longer exist: the server rejects unknown
		// groups, and the modal cannot show a checkbox for a deleted group.
		const knownGroupIds = new Set(hostGroups.map((g) => g.id));
		const groupIds =
			hostGroups.length > 0
				? form.host_group_ids.filter((id) => knownGroupIds.has(id))
				: form.host_group_ids;
		let recipients = form.email_recipients;
		if (isCustomer) {
			if (recipientInput.trim()) {
				const added = addRecipients(recipients, recipientInput);
				if (added.error) {
					toast.warning(added.error);
					return;
				}
				recipients = added.list;
				upd("email_recipients", recipients);
				setRecipientInput("");
			}
			if (recipients.length === 0) {
				toast.warning("Add at least one recipient");
				return;
			}
			if (!smtpValid) {
				toast.warning("Select an SMTP account");
				return;
			}
			if (groupIds.length === 0) {
				toast.warning("Customer reports need at least one host group");
				return;
			}
		}
		const body = {
			name: form.name.trim(),
			cron_expr: cronExpr,
			enabled: form.enabled,
			definition: {
				version: 2,
				sections: form.sections,
				host_group_ids: groupIds,
				language: form.language,
				period_days: Number(form.period_days) || 30,
				limits: { top_hosts: Number(form.top_hosts) || 20 },
			},
			destination_ids: isCustomer
				? [form.smtp_destination_id]
				: destinations.length > 0
					? form.destination_ids.filter((id) => reportDestinationIds.has(id))
					: form.destination_ids,
			email_recipients: isCustomer ? recipients : null,
		};
		if (isCustomer && form.enabled) {
			setConfirmBody(body);
			return;
		}
		onSave(body);
	};

	const saveLabel = editingReport && !isDuplicate ? "Save" : "Create";

	return (
		<>
			<div
				className="fixed inset-0 bg-black/50 flex items-center justify-center z-50"
				onClick={onClose}
			>
				<div
					className="bg-white dark:bg-secondary-800 rounded-lg shadow-xl max-w-lg w-full mx-4 relative z-10 max-h-[90vh] overflow-y-auto"
					onClick={(e) => e.stopPropagation()}
				>
					<div className="px-6 py-4 border-b border-secondary-200 dark:border-secondary-600 flex items-center justify-between sticky top-0 bg-white dark:bg-secondary-800 z-10">
						<h3 className="text-lg font-semibold text-secondary-900 dark:text-white">
							{isDuplicate
								? "Duplicate report"
								: editingReport
									? "Edit report"
									: "New scheduled report"}
						</h3>
						<button
							type="button"
							onClick={onClose}
							className="text-secondary-400 hover:text-secondary-600 dark:hover:text-white"
						>
							<X className="h-5 w-5" />
						</button>
					</div>
					<div className="px-6 py-5 space-y-5">
						<div className="grid grid-cols-2 gap-2">
							{[
								{
									id: "internal",
									label: "Internal report",
									hint: "To your notification destinations",
								},
								{
									id: "customer",
									label: "Customer report",
									hint: "PDF by e-mail to external recipients",
								},
							].map((m) => (
								<button
									key={m.id}
									type="button"
									aria-pressed={form.mode === m.id}
									className={`text-left px-3 py-2 rounded-md border transition-colors ${
										form.mode === m.id
											? "border-primary-600 bg-primary-50 dark:bg-primary-900/30"
											: "border-secondary-300 dark:border-secondary-600 hover:border-primary-400"
									}`}
									onClick={() => switchMode(m.id)}
								>
									<span className="block text-sm font-medium text-secondary-900 dark:text-white">
										{m.label}
									</span>
									<span className="block text-xs text-secondary-500">
										{m.hint}
									</span>
								</button>
							))}
						</div>

						<div>
							<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-1">
								Report name <span className="text-danger-500">*</span>
							</label>
							<input
								className={INPUT}
								placeholder="Weekly ops report"
								value={form.name}
								onChange={(e) => upd("name", e.target.value)}
							/>
						</div>

						<div>
							<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-2">
								Schedule
							</label>
							<div className="flex flex-wrap gap-3 items-center">
								<select
									className={`${SELECT} w-auto`}
									value={form.frequency}
									onChange={(e) => upd("frequency", e.target.value)}
								>
									{FREQUENCY_OPTIONS.map((p) => (
										<option key={p.value} value={p.value}>
											{p.label}
										</option>
									))}
								</select>
								<span className="text-sm text-secondary-500">at</span>
								<input
									type="time"
									className={`${INPUT} w-auto`}
									value={form.time}
									onChange={(e) => upd("time", e.target.value)}
								/>
							</div>
							{form.frequency === "weekly" && (
								<div className="flex gap-1.5 mt-3">
									{DAY_LABELS.map((d) => (
										<button
											key={d.value}
											type="button"
											className={`px-3 py-1.5 text-xs font-medium rounded-md border transition-colors ${
												form.days.includes(d.value)
													? "bg-primary-600 text-white border-primary-600"
													: "bg-white dark:bg-secondary-900 text-secondary-700 dark:text-secondary-300 border-secondary-300 dark:border-secondary-600 hover:border-primary-400"
											}`}
											onClick={() => toggleDay(d.value)}
										>
											{d.short}
										</button>
									))}
								</div>
							)}
							{form.frequency === "monthly" && (
								<div className="mt-3 space-y-2">
									<div className="flex gap-1.5 flex-wrap">
										{MONTH_DAY_PRESETS.map((p) => (
											<button
												key={p.value}
												type="button"
												className={`px-3 py-1.5 text-xs font-medium rounded-md border transition-colors ${
													form.monthDay === p.value
														? "bg-primary-600 text-white border-primary-600"
														: "bg-white dark:bg-secondary-900 text-secondary-700 dark:text-secondary-300 border-secondary-300 dark:border-secondary-600 hover:border-primary-400"
												}`}
												onClick={() => upd("monthDay", p.value)}
											>
												{p.label}
											</button>
										))}
										<span className="text-sm text-secondary-500 self-center px-1">
											or
										</span>
										<input
											type="number"
											min={1}
											max={31}
											placeholder="Day"
											className={`${INPUT} w-20 text-center`}
											value={
												!["1", "15", "L"].includes(form.monthDay)
													? form.monthDay
													: ""
											}
											onChange={(e) => {
												const v = e.target.value;
												if (v === "") return;
												const n = Math.max(1, Math.min(31, Number(v) || 1));
												upd("monthDay", String(n));
											}}
											onFocus={() => {
												if (["1", "15", "L"].includes(form.monthDay))
													upd("monthDay", "");
											}}
										/>
									</div>
								</div>
							)}
							<p className="mt-2 text-xs text-secondary-500 flex items-center gap-1">
								<Clock className="h-3 w-3" /> Server timezone
							</p>
						</div>

						<div>
							<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-2">
								Sections
							</label>
							<div className="grid grid-cols-2 gap-2">
								{REPORT_SECTIONS.map((s) => (
									<label
										key={s.id}
										className="flex items-center gap-2 text-sm text-secondary-700 dark:text-white"
									>
										<input
											type="checkbox"
											checked={form.sections.includes(s.id)}
											onChange={() => toggleArr("sections", s.id)}
										/>
										{s.label}
									</label>
								))}
							</div>
						</div>

						{isCustomer && (
							<>
								<div>
									<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-1">
										Recipients <span className="text-danger-500">*</span>
									</label>
									{form.email_recipients.length > 0 && (
										<div className="flex flex-wrap gap-1.5 mb-2">
											{form.email_recipients.map((addr) => (
												<span
													key={addr}
													className="inline-flex items-center gap-1 pl-2 pr-1 py-0.5 rounded-md text-xs bg-secondary-100 text-secondary-800 dark:bg-secondary-700 dark:text-secondary-100"
												>
													{addr}
													<button
														type="button"
														className="p-0.5 rounded hover:bg-secondary-200 dark:hover:bg-secondary-600"
														onClick={() => removeRecipient(addr)}
														aria-label={`Remove ${addr}`}
													>
														<X className="h-3 w-3" />
													</button>
												</span>
											))}
										</div>
									)}
									<input
										className={INPUT}
										type="text"
										inputMode="email"
										autoComplete="off"
										placeholder={
											form.email_recipients.length >= MAX_RECIPIENTS
												? `At most ${MAX_RECIPIENTS} recipients`
												: "name@customer.example, then Enter"
										}
										disabled={form.email_recipients.length >= MAX_RECIPIENTS}
										value={recipientInput}
										onChange={(e) => setRecipientInput(e.target.value)}
										onKeyDown={(e) => {
											if (e.key === "Enter" || e.key === ",") {
												e.preventDefault();
												commitRecipientInput();
											}
										}}
										onBlur={() => commitRecipientInput({ silent: true })}
									/>
									<p className="mt-1 text-xs text-secondary-500">
										Up to {MAX_RECIPIENTS} addresses. Each recipient gets their
										own e-mail.
									</p>
								</div>

								<div>
									<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-1">
										SMTP account <span className="text-danger-500">*</span>
									</label>
									{emailAccounts.length === 0 ? (
										<p className="text-xs text-secondary-500">
											Add an enabled e-mail destination first.
										</p>
									) : (
										<select
											className={SELECT}
											value={smtpValid ? form.smtp_destination_id : ""}
											onChange={(e) =>
												upd("smtp_destination_id", e.target.value)
											}
										>
											<option value="" disabled>
												Select an SMTP account
											</option>
											{emailAccounts.map((d) => (
												<option key={d.id} value={d.id}>
													{d.display_name}
												</option>
											))}
										</select>
									)}
									<p className="mt-1 text-xs text-secondary-500">
										The account must have &quot;Use TLS&quot; enabled.
									</p>
									<p className="mt-1 text-xs text-secondary-500">
										The account&apos;s own To address is ignored; the report
										goes to the recipients above.
									</p>
								</div>

								<div>
									<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-2">
										Host groups <span className="text-danger-500">*</span>
									</label>
									{hostGroups.length === 0 ? (
										<p className="text-xs text-secondary-500">
											Create a host group first. A customer report only covers
											the hosts of its groups.
										</p>
									) : (
										<div className="space-y-1.5">
											{hostGroups.map((g) => (
												<label
													key={g.id}
													className="flex items-center gap-2 text-sm text-secondary-700 dark:text-white"
												>
													<input
														type="checkbox"
														checked={form.host_group_ids.includes(g.id)}
														onChange={() => toggleArr("host_group_ids", g.id)}
													/>
													{g.name || g.id}
												</label>
											))}
										</div>
									)}
								</div>
							</>
						)}

						{!isCustomer && (
							<>
								<div>
									<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-2">
										Deliver to
									</label>
									{reportDestinations.length === 0 ? (
										<p className="text-xs text-secondary-500">
											Add a destination first.
										</p>
									) : (
										<div className="space-y-1.5">
											{reportDestinations.map((d) => (
												<label
													key={d.id}
													className="flex items-center gap-2 text-sm text-secondary-700 dark:text-white"
												>
													<input
														type="checkbox"
														checked={form.destination_ids.includes(d.id)}
														onChange={() => toggleArr("destination_ids", d.id)}
													/>
													{channelIcon(d.channel_type)}
													{d.display_name}
												</label>
											))}
										</div>
									)}
								</div>

								{hostGroups.length > 0 && (
									<div>
										<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-2">
											Scope to host groups
										</label>
										<div className="space-y-1.5">
											{hostGroups.map((g) => (
												<label
													key={g.id}
													className="flex items-center gap-2 text-sm text-secondary-700 dark:text-white"
												>
													<input
														type="checkbox"
														checked={form.host_group_ids.includes(g.id)}
														onChange={() => toggleArr("host_group_ids", g.id)}
													/>
													{g.name || g.id}
												</label>
											))}
										</div>
									</div>
								)}
							</>
						)}

						<div className="grid grid-cols-2 gap-4">
							<div>
								<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-1">
									Language
								</label>
								<select
									className={INPUT}
									value={form.language}
									onChange={(e) => upd("language", e.target.value)}
								>
									{REPORT_LANGUAGES.map((l) => (
										<option key={l.id} value={l.id}>
											{l.label}
										</option>
									))}
								</select>
							</div>
							<div>
								<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-1">
									Activity period
								</label>
								<select
									className={INPUT}
									value={form.period_days}
									onChange={(e) => upd("period_days", Number(e.target.value))}
								>
									{REPORT_PERIODS.map((d) => (
										<option key={d} value={d}>
											Last {d} days
										</option>
									))}
								</select>
								<p className="mt-1 text-xs text-secondary-500">
									Window for patch activity, reboots and the patching KPIs;
									independent of the delivery schedule.
								</p>
							</div>
						</div>

						<div className="grid grid-cols-2 gap-4">
							<div>
								<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-1">
									Top rows per section
								</label>
								<input
									className={INPUT}
									type="number"
									min={1}
									value={form.top_hosts}
									onChange={(e) => upd("top_hosts", e.target.value)}
								/>
							</div>
							<div className="flex items-end pb-1">
								<label className="flex items-center gap-2 text-sm text-secondary-700 dark:text-white">
									<input
										type="checkbox"
										checked={form.enabled}
										onChange={(e) => upd("enabled", e.target.checked)}
									/>
									Enabled
								</label>
							</div>
						</div>

						{isCustomer && (
							<p className="text-xs text-secondary-500">
								The PDF header uses the uploaded light logo when it is PNG or
								JPEG. SVG logos fall back to the default logo.
							</p>
						)}
					</div>
					<div className="px-6 py-4 border-t border-secondary-200 dark:border-secondary-600 flex justify-end gap-2 sticky bottom-0 bg-white dark:bg-secondary-800">
						<button type="button" className="btn-outline" onClick={onClose}>
							Cancel
						</button>
						<button
							type="button"
							className="btn-primary flex items-center gap-1"
							disabled={isPending}
							onClick={handleSave}
						>
							{isPending ? (
								<Loader2 className="h-4 w-4 animate-spin" />
							) : (
								<Check className="h-4 w-4" />
							)}
							{saveLabel}
						</button>
					</div>
				</div>
			</div>
			<ReportConfirmDialog
				open={confirmBody !== null}
				title="Send reports to external recipients?"
				summary={
					confirmBody
						? buildReportSummary({
								groupIds: confirmBody.definition.host_group_ids,
								hostGroups,
								hosts,
								recipients: confirmBody.email_recipients,
								destinations,
								smtpId: confirmBody.destination_ids[0],
								language: confirmBody.definition.language,
							})
						: null
				}
				confirmLabel={saveLabel}
				onConfirm={() => {
					const body = confirmBody;
					setConfirmBody(null);
					onSave(body);
				}}
				onCancel={() => setConfirmBody(null)}
			/>
		</>
	);
};

/* ───────────────── Main Page ───────────────── */

export default ReportModal;
