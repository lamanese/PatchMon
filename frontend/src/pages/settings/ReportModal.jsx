import { Bell, Check, Clock, Globe, Loader2, Mail, X } from "lucide-react";
import { useState } from "react";
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

const ReportModal = ({
	isOpen,
	onClose,
	onSave,
	editingReport,
	destinations,
	hostGroups,
	isPending,
}) => {
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
		name: editingReport?.name || "",
		frequency: cronInit.frequency,
		time: cronInit.time,
		days: cronInit.days,
		monthDay: cronInit.monthDay,
		enabled: editingReport?.enabled !== false,
		destination_ids: Array.isArray(editingReport?.destination_ids)
			? editingReport.destination_ids
			: [],
		sections:
			Array.isArray(defRow.sections) && defRow.sections.length > 0
				? defRow.sections
				: ["executive_summary", "compliance_summary", "recent_patch_runs"],
		host_group_ids: Array.isArray(defRow.host_group_ids)
			? defRow.host_group_ids
			: [],
		top_hosts: defRow.limits?.top_hosts ?? 20,
		language: defRow.language === "de" ? "de" : "en",
		period_days: REPORT_PERIODS.includes(Number(defRow.period_days))
			? Number(defRow.period_days)
			: 30,
	});
	const toast = useToast();

	if (!isOpen) return null;

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
		onSave({
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
			destination_ids: form.destination_ids,
		});
	};

	return (
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
						{editingReport ? "Edit report" : "New scheduled report"}
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

					<div>
						<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-2">
							Deliver to
						</label>
						{destinations.length === 0 ? (
							<p className="text-xs text-secondary-500">
								Add a destination first.
							</p>
						) : (
							<div className="space-y-1.5">
								{destinations.map((d) => (
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
						{editingReport ? "Save" : "Create"}
					</button>
				</div>
			</div>
		</div>
	);
};

/* ───────────────── Main Page ───────────────── */

export default ReportModal;
