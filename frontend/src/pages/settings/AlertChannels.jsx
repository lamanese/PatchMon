import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
	Check,
	ChevronLeft,
	ChevronRight,
	Clock,
	Edit2,
	FileText,
	Loader2,
	Play,
	Plus,
	RefreshCw,
	Send,
	Trash2,
	X,
} from "lucide-react";
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useAuth } from "../../contexts/AuthContext";
import { useToast } from "../../contexts/ToastContext";
import {
	adminHostsAPI,
	formatRelativeTime,
	hostGroupsAPI,
	notificationsAPI,
} from "../../utils/api";
import ReportModal, {
	CHANNEL_TYPES,
	channelIcon,
	describeSchedule,
	INPUT,
	SELECT,
} from "./ReportModal";

/* ───────────────────── Constants ───────────────────── */

const EVENT_TYPES = [
	{ value: "*", label: "All events" },
	{ value: "host_down", label: "Host down" },
	{ value: "host_recovered", label: "Host recovered / up" },
	{ value: "host_enrolled", label: "Host enrolled" },
	{ value: "host_deleted", label: "Host deleted" },
	{ value: "server_update", label: "Server update" },
	{ value: "agent_update", label: "Agent update" },
	{ value: "patch_run_started", label: "Patch run started" },
	{ value: "patch_run_completed", label: "Patch run completed" },
	{ value: "patch_run_failed", label: "Patch run failed" },
	{ value: "patch_run_approved", label: "Patch run approved" },
	{ value: "patch_run_cancelled", label: "Patch run cancelled" },
	{ value: "patch_reboot_required", label: "Reboot required" },
	{ value: "compliance_scan_completed", label: "Compliance scan completed" },
	{ value: "compliance_scan_failed", label: "Compliance scan failed" },
	{ value: "container_stopped", label: "Container stopped" },
	{ value: "container_started", label: "Container started" },
	{
		value: "container_image_update_available",
		label: "Container image update available",
	},
	{ value: "ssh_session_started", label: "SSH session started" },
	{ value: "rdp_session_started", label: "RDP session started" },
	{
		value: "host_security_updates_exceeded",
		label: "Security updates threshold exceeded",
	},
	{
		value: "host_pending_updates_exceeded",
		label: "Pending updates threshold exceeded",
	},
	{ value: "user_login", label: "User login" },
	{ value: "user_login_failed", label: "Failed login attempt" },
	{ value: "account_locked", label: "Account locked" },
	{ value: "user_created", label: "User created" },
	{ value: "user_role_changed", label: "User role changed" },
	{ value: "user_tfa_disabled", label: "2FA disabled" },
];

const SEVERITIES = [
	{ value: "informational", label: "Informational" },
	{ value: "warning", label: "Warning" },
	{ value: "error", label: "Error" },
	{ value: "critical", label: "Critical" },
];

const statusBadge = (status) => {
	const ok = status === "sent";
	return (
		<span
			className={`px-2 py-0.5 text-xs font-medium rounded-md ${ok ? "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200" : "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200"}`}
		>
			{status}
		</span>
	);
};

/* ───────────────── Destination Modal ───────────────── */

const DestinationModal = ({
	isOpen,
	onClose,
	onSave,
	editingDest,
	isPending,
}) => {
	const [step, setStep] = useState(editingDest ? 2 : 1);
	const [channelType, setChannelType] = useState(
		editingDest?.channel_type || "",
	);
	const [displayName, setDisplayName] = useState(
		editingDest?.display_name || "",
	);
	const [enabled, setEnabled] = useState(editingDest?.enabled !== false);
	const [config, setConfig] = useState(editingDest?._loadedConfig || {});
	const toast = useToast();

	if (!isOpen) return null;

	const handleSave = () => {
		if (!displayName.trim()) {
			toast.warning("Display name is required");
			return;
		}
		if (channelType === "webhook" && !config.url) {
			toast.warning("Webhook URL is required");
			return;
		}
		if (
			channelType === "email" &&
			(!config.smtp_host || !config.from || !config.to)
		) {
			toast.warning("SMTP host, from, and to are required");
			return;
		}
		if (channelType === "ntfy" && !config.topic) {
			toast.warning("Topic is required");
			return;
		}
		onSave({
			channel_type: channelType,
			display_name: displayName.trim(),
			config,
			enabled,
		});
	};

	const updateConfig = (key, value) =>
		setConfig((p) => ({ ...p, [key]: value }));

	const renderFields = () => {
		switch (channelType) {
			case "webhook":
				return (
					<div className="space-y-4">
						<div>
							<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-1">
								Webhook URL <span className="text-danger-500">*</span>
							</label>
							<input
								className={INPUT}
								placeholder="https://hooks.slack.com/services/... or https://discord.com/api/webhooks/..."
								value={config.url || ""}
								onChange={(e) => updateConfig("url", e.target.value)}
							/>
							<p className="mt-1 text-xs text-secondary-500">
								Discord and Slack URLs are auto-detected for rich formatting
							</p>
						</div>
						<div>
							<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-1">
								Signing secret
							</label>
							<input
								className={INPUT}
								type="password"
								placeholder="Optional HMAC signing secret"
								value={config.signing_secret || ""}
								onChange={(e) => updateConfig("signing_secret", e.target.value)}
							/>
						</div>
					</div>
				);
			case "email":
				return (
					<div className="space-y-4">
						<div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
							<div>
								<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-1">
									SMTP host <span className="text-danger-500">*</span>
								</label>
								<input
									className={INPUT}
									placeholder="smtp.example.com"
									value={config.smtp_host || ""}
									onChange={(e) => updateConfig("smtp_host", e.target.value)}
								/>
							</div>
							<div>
								<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-1">
									SMTP port
								</label>
								<input
									className={INPUT}
									type="number"
									placeholder="587"
									value={config.smtp_port || 587}
									onChange={(e) =>
										updateConfig("smtp_port", Number(e.target.value) || 587)
									}
								/>
							</div>
						</div>
						<div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
							<div>
								<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-1">
									Username
								</label>
								<input
									className={INPUT}
									value={config.username || ""}
									onChange={(e) => updateConfig("username", e.target.value)}
								/>
							</div>
							<div>
								<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-1">
									Password
								</label>
								<input
									className={INPUT}
									type="password"
									value={config.password || ""}
									onChange={(e) => updateConfig("password", e.target.value)}
								/>
							</div>
						</div>
						<div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
							<div>
								<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-1">
									From <span className="text-danger-500">*</span>
								</label>
								<input
									className={INPUT}
									placeholder="noreply@example.com"
									value={config.from || ""}
									onChange={(e) => updateConfig("from", e.target.value)}
								/>
							</div>
							<div>
								<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-1">
									To <span className="text-danger-500">*</span>
								</label>
								<input
									className={INPUT}
									placeholder="team@example.com"
									value={config.to || ""}
									onChange={(e) => updateConfig("to", e.target.value)}
								/>
							</div>
						</div>
						<label className="flex items-center gap-2 text-sm text-secondary-700 dark:text-white">
							<input
								type="checkbox"
								checked={config.use_tls !== false}
								onChange={(e) => updateConfig("use_tls", e.target.checked)}
							/>
							Use TLS
						</label>
					</div>
				);
			case "ntfy":
				return (
					<div className="space-y-4">
						<div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
							<div>
								<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-1">
									Server URL
								</label>
								<input
									className={INPUT}
									placeholder="https://ntfy.sh"
									value={config.server_url || ""}
									onChange={(e) => updateConfig("server_url", e.target.value)}
								/>
								<p className="mt-1 text-xs text-secondary-500">
									Leave empty for ntfy.sh
								</p>
							</div>
							<div>
								<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-1">
									Topic <span className="text-danger-500">*</span>
								</label>
								<input
									className={INPUT}
									placeholder="patchmon-alerts"
									value={config.topic || ""}
									onChange={(e) => updateConfig("topic", e.target.value)}
								/>
							</div>
						</div>
						<div>
							<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-1">
								Access token
							</label>
							<input
								className={INPUT}
								type="password"
								placeholder="Optional"
								value={config.token || ""}
								onChange={(e) => updateConfig("token", e.target.value)}
							/>
						</div>
						<div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
							<div>
								<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-1">
									Username
								</label>
								<input
									className={INPUT}
									placeholder="Optional basic auth"
									value={config.username || ""}
									onChange={(e) => updateConfig("username", e.target.value)}
								/>
							</div>
							<div>
								<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-1">
									Password
								</label>
								<input
									className={INPUT}
									type="password"
									value={config.password || ""}
									onChange={(e) => updateConfig("password", e.target.value)}
								/>
							</div>
						</div>
					</div>
				);
			default:
				return null;
		}
	};

	return (
		<div
			className="fixed inset-0 bg-black/50 flex items-center justify-center z-50"
			onClick={onClose}
		>
			<div
				className="bg-white dark:bg-secondary-800 rounded-lg shadow-xl max-w-lg w-full mx-4 relative z-10"
				onClick={(e) => e.stopPropagation()}
			>
				<div className="px-6 py-4 border-b border-secondary-200 dark:border-secondary-600 flex items-center justify-between">
					<h3 className="text-lg font-semibold text-secondary-900 dark:text-white">
						{editingDest
							? "Edit destination"
							: step === 1
								? "Choose channel"
								: "Configure destination"}
					</h3>
					<button
						type="button"
						onClick={onClose}
						className="text-secondary-400 hover:text-secondary-600 dark:hover:text-white"
					>
						<X className="h-5 w-5" />
					</button>
				</div>

				<div className="px-6 py-5">
					{step === 1 && !editingDest && (
						<div className="grid grid-cols-3 gap-4">
							{CHANNEL_TYPES.filter((ct) => ct.value !== "internal").map(
								(ct) => {
									const Icon = ct.icon;
									return (
										<button
											key={ct.value}
											type="button"
											className={`flex flex-col items-center justify-center p-6 rounded-lg border-2 transition-all ${
												channelType === ct.value
													? "border-primary-500 bg-primary-50 dark:bg-primary-900/30"
													: "border-secondary-300 dark:border-secondary-600 hover:border-primary-400"
											}`}
											onClick={() => setChannelType(ct.value)}
										>
											<Icon className="h-10 w-10 text-secondary-700 dark:text-secondary-200 mb-2" />
											<span className="text-sm font-medium text-secondary-900 dark:text-white">
												{ct.label}
											</span>
											<span className="text-xs text-secondary-500 mt-1 text-center">
												{ct.description}
											</span>
										</button>
									);
								},
							)}
						</div>
					)}

					{step === 2 && (
						<div className="space-y-5">
							<div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
								<div>
									<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-1">
										Display name <span className="text-danger-500">*</span>
									</label>
									<input
										className={INPUT}
										placeholder="e.g. Ops Discord"
										value={displayName}
										onChange={(e) => setDisplayName(e.target.value)}
									/>
								</div>
								<div className="flex items-end pb-1">
									<label className="flex items-center gap-2 text-sm text-secondary-700 dark:text-white">
										<div
											className={`relative inline-flex h-5 w-9 items-center rounded-md transition-colors ${enabled ? "bg-primary-600 dark:bg-primary-500" : "bg-secondary-200 dark:bg-secondary-600"}`}
											onClick={() => setEnabled(!enabled)}
											onKeyDown={() => {}}
										>
											<span
												className={`inline-block h-3 w-3 transform rounded-md bg-white transition-transform ${enabled ? "translate-x-5" : "translate-x-1"}`}
											/>
										</div>
										Enabled
									</label>
								</div>
							</div>
							{renderFields()}
						</div>
					)}
				</div>

				<div className="px-6 py-4 border-t border-secondary-200 dark:border-secondary-600 flex justify-between">
					{step === 2 && !editingDest ? (
						<button
							type="button"
							className="btn-outline flex items-center gap-1"
							onClick={() => setStep(1)}
						>
							<ChevronLeft className="h-4 w-4" /> Back
						</button>
					) : (
						<div />
					)}
					<div className="flex gap-2">
						<button type="button" className="btn-outline" onClick={onClose}>
							Cancel
						</button>
						{step === 1 && (
							<button
								type="button"
								className="btn-primary"
								disabled={!channelType}
								onClick={() => setStep(2)}
							>
								Next <ChevronRight className="h-4 w-4 inline ml-1" />
							</button>
						)}
						{step === 2 && (
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
								{editingDest ? "Save" : "Create"}
							</button>
						)}
					</div>
				</div>
			</div>
		</div>
	);
};

/* ───────────────── Route Modal ───────────────── */

const RouteModal = ({
	isOpen,
	onClose,
	onSave,
	editingRoute,
	destinations,
	hostGroups,
	hosts,
	isPending,
}) => {
	const parseArr = (v) => (Array.isArray(v) ? v : []);
	const [form, setForm] = useState({
		destination_id: editingRoute?.destination_id || "",
		event_types:
			parseArr(editingRoute?.event_types).length > 0
				? parseArr(editingRoute.event_types)
				: ["*"],
		min_severity: editingRoute?.min_severity || "informational",
		host_group_ids: parseArr(editingRoute?.host_group_ids),
		host_ids: parseArr(editingRoute?.host_ids),
		enabled: editingRoute?.enabled !== false,
	});
	const toast = useToast();

	if (!isOpen) return null;

	const handleSave = () => {
		if (!form.destination_id) {
			toast.warning("Choose a destination");
			return;
		}
		onSave({
			destination_id: form.destination_id,
			event_types: form.event_types.length > 0 ? form.event_types : ["*"],
			min_severity: form.min_severity,
			host_group_ids: form.host_group_ids,
			host_ids: form.host_ids,
			enabled: form.enabled,
		});
	};

	const upd = (key, value) => setForm((p) => ({ ...p, [key]: value }));
	const toggleArr = (key, id) =>
		setForm((p) => ({
			...p,
			[key]: p[key].includes(id)
				? p[key].filter((x) => x !== id)
				: [...p[key], id],
		}));

	const allEventValues = EVENT_TYPES.filter((e) => e.value !== "*").map(
		(e) => e.value,
	);

	const toggleEvent = (value) => {
		if (value === "*") {
			// Toggle: if all selected, clear all; if not all, select all
			setForm((p) =>
				p.event_types.includes("*")
					? { ...p, event_types: [] }
					: { ...p, event_types: ["*"] },
			);
			return;
		}
		setForm((p) => {
			// If currently "all", expand to individual events then remove the clicked one
			const next = p.event_types.includes("*")
				? allEventValues.filter((v) => v !== value)
				: p.event_types.includes(value)
					? p.event_types.filter((x) => x !== value)
					: [...p.event_types, value];
			// If all individual events are selected, collapse back to wildcard
			if (next.length >= allEventValues.length)
				return { ...p, event_types: ["*"] };
			return { ...p, event_types: next.length > 0 ? next : ["*"] };
		});
	};

	const allEvents = form.event_types.includes("*");

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
						{editingRoute ? "Edit route" : "Add route"}
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
							Destination <span className="text-danger-500">*</span>
						</label>
						<select
							className={SELECT}
							value={form.destination_id}
							onChange={(e) => upd("destination_id", e.target.value)}
						>
							<option value="">Select destination...</option>
							{destinations.map((d) => (
								<option key={d.id} value={d.id}>
									{d.display_name} ({d.channel_type})
								</option>
							))}
						</select>
					</div>

					<div>
						<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-2">
							Events
						</label>
						<div className="space-y-1.5">
							<label className="flex items-center gap-2 text-sm text-secondary-700 dark:text-white font-medium">
								<input
									type="checkbox"
									checked={allEvents}
									onChange={() => toggleEvent("*")}
								/>
								All events
							</label>
							<div className="grid grid-cols-2 gap-1.5 pl-4 pt-1">
								{EVENT_TYPES.filter((e) => e.value !== "*").map((o) => (
									<label
										key={o.value}
										className="flex items-center gap-2 text-sm text-secondary-700 dark:text-white"
									>
										<input
											type="checkbox"
											checked={allEvents || form.event_types.includes(o.value)}
											onChange={() => toggleEvent(o.value)}
										/>
										{o.label}
									</label>
								))}
							</div>
						</div>
					</div>

					<div>
						<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-1">
							Minimum severity
						</label>
						<select
							className={SELECT}
							value={form.min_severity}
							onChange={(e) => upd("min_severity", e.target.value)}
						>
							{SEVERITIES.map((o) => (
								<option key={o.value} value={o.value}>
									{o.label}
								</option>
							))}
						</select>
					</div>

					{hostGroups.length > 0 && (
						<div>
							<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-2">
								Host groups{" "}
								<span className="text-xs font-normal text-secondary-500">
									(optional, leave empty for all)
								</span>
							</label>
							<div className="space-y-1.5 max-h-40 overflow-y-auto">
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

					{hosts.length > 0 && (
						<div>
							<label className="block text-sm font-medium text-secondary-700 dark:text-white mb-2">
								Individual hosts{" "}
								<span className="text-xs font-normal text-secondary-500">
									(optional, leave empty for all)
								</span>
							</label>
							<div className="space-y-1.5 max-h-40 overflow-y-auto">
								{hosts.map((h) => (
									<label
										key={h.id}
										className="flex items-center gap-2 text-sm text-secondary-700 dark:text-white"
									>
										<input
											type="checkbox"
											checked={form.host_ids.includes(h.id)}
											onChange={() => toggleArr("host_ids", h.id)}
										/>
										{h.friendly_name || h.hostname || h.id}
									</label>
								))}
							</div>
						</div>
					)}

					<label className="flex items-center gap-2 text-sm text-secondary-700 dark:text-white">
						<input
							type="checkbox"
							checked={form.enabled}
							onChange={(e) => upd("enabled", e.target.checked)}
						/>
						Enabled
					</label>
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
						{editingRoute ? "Save" : "Add"}
					</button>
				</div>
			</div>
		</div>
	);
};

/** Renders notification management for a specific panel. Used by Reporting page tabs. */
export const NotificationPanel = ({ panel }) => {
	const queryClient = useQueryClient();
	const toast = useToast();
	const { canManageNotifications, canViewNotificationLogs, hasPermission } =
		useAuth();
	const canManage = canManageNotifications();
	const canLog = canViewNotificationLogs();
	const canListHostGroups = hasPermission("can_view_hosts");

	// Modal states
	const [destModal, setDestModal] = useState({ open: false, editing: null });
	const [routeModal, setRouteModal] = useState({ open: false, editing: null });
	const [reportModal, setReportModal] = useState({
		open: false,
		editing: null,
	});
	const [logPage, setLogPage] = useState(0);
	const logPageSize = 50;
	const [previewingId, setPreviewingId] = useState(null);

	// Queries
	const { data: destinations = [], isLoading: destLoading } = useQuery({
		queryKey: ["notifications", "destinations"],
		queryFn: () => notificationsAPI.listDestinations().then((r) => r.data),
		enabled: canManage,
	});
	const { data: routes = [], isLoading: routesLoading } = useQuery({
		queryKey: ["notifications", "routes"],
		queryFn: () => notificationsAPI.listRoutes().then((r) => r.data),
		enabled: canManage,
	});
	const { data: deliveryLog = [], isLoading: logLoading } = useQuery({
		queryKey: ["notifications", "delivery-log", logPage],
		queryFn: () =>
			notificationsAPI
				.listDeliveryLog({ limit: logPageSize, offset: logPage * logPageSize })
				.then((r) => r.data),
		enabled: canLog,
	});
	const { data: scheduledReports = [], isLoading: reportsLoading } = useQuery({
		queryKey: ["notifications", "scheduled-reports"],
		queryFn: () => notificationsAPI.listScheduledReports().then((r) => r.data),
		enabled: canManage,
	});
	const { data: hostGroups = [] } = useQuery({
		queryKey: ["host-groups"],
		queryFn: () => hostGroupsAPI.list().then((r) => r.data ?? []),
		enabled: canManage && canListHostGroups,
	});
	const { data: hostsList = [] } = useQuery({
		queryKey: ["hosts-list"],
		queryFn: () => adminHostsAPI.list().then((r) => r.data ?? []),
		enabled: canManage && canListHostGroups,
	});

	const hostGroupOptions = useMemo(
		() => (Array.isArray(hostGroups) ? hostGroups : []),
		[hostGroups],
	);
	const destNameMap = useMemo(() => {
		const m = {};
		for (const d of destinations) m[d.id] = d.display_name;
		return m;
	}, [destinations]);
	const hostGroupNameMap = useMemo(() => {
		const m = {};
		for (const g of hostGroupOptions) m[g.id] = g.name || g.id;
		return m;
	}, [hostGroupOptions]);

	const invalidate = () =>
		queryClient.invalidateQueries({ queryKey: ["notifications"] });

	// Mutations
	const createDest = useMutation({
		mutationFn: (body) => notificationsAPI.createDestination(body),
		onSuccess: () => {
			invalidate();
			toast.success("Destination created");
			setDestModal({ open: false, editing: null });
		},
		onError: (err) =>
			toast.error(err.response?.data?.error || "Failed to create"),
	});
	const updateDest = useMutation({
		mutationFn: ({ id, body }) => notificationsAPI.updateDestination(id, body),
		onSuccess: () => {
			invalidate();
			toast.success("Destination updated");
			setDestModal({ open: false, editing: null });
		},
		onError: (err) =>
			toast.error(err.response?.data?.error || "Failed to update"),
	});
	const deleteDest = useMutation({
		mutationFn: (id) => notificationsAPI.deleteDestination(id),
		onSuccess: () => {
			invalidate();
			toast.success("Destination deleted");
		},
		onError: (err) =>
			toast.error(err.response?.data?.error || "Failed to delete"),
	});
	const testNotify = useMutation({
		mutationFn: (destination_id) => notificationsAPI.test({ destination_id }),
	});

	const createRoute = useMutation({
		mutationFn: (body) => notificationsAPI.createRoute(body),
		onSuccess: () => {
			invalidate();
			toast.success("Route created");
			setRouteModal({ open: false, editing: null });
		},
		onError: (err) =>
			toast.error(err.response?.data?.error || "Failed to create"),
	});
	const updateRoute = useMutation({
		mutationFn: ({ id, body }) => notificationsAPI.updateRoute(id, body),
		onSuccess: () => {
			invalidate();
			toast.success("Route updated");
			setRouteModal({ open: false, editing: null });
		},
		onError: (err) =>
			toast.error(err.response?.data?.error || "Failed to update"),
	});
	const deleteRoute = useMutation({
		mutationFn: (id) => notificationsAPI.deleteRoute(id),
		onSuccess: () => {
			invalidate();
			toast.success("Route deleted");
		},
		onError: (err) =>
			toast.error(err.response?.data?.error || "Failed to delete"),
	});

	const createReport = useMutation({
		mutationFn: (body) => notificationsAPI.createScheduledReport(body),
		onSuccess: () => {
			invalidate();
			toast.success("Report created");
			setReportModal({ open: false, editing: null });
		},
		onError: (err) =>
			toast.error(err.response?.data?.error || "Failed to create"),
	});
	const updateReport = useMutation({
		mutationFn: ({ id, body }) =>
			notificationsAPI.updateScheduledReport(id, body),
		onSuccess: () => {
			invalidate();
			toast.success("Report updated");
			setReportModal({ open: false, editing: null });
		},
		onError: (err) =>
			toast.error(err.response?.data?.error || "Failed to update"),
	});
	const deleteReport = useMutation({
		mutationFn: (id) => notificationsAPI.deleteScheduledReport(id),
		onSuccess: () => {
			invalidate();
			toast.success("Report deleted");
		},
		onError: (err) =>
			toast.error(err.response?.data?.error || "Failed to delete"),
	});
	const runReportNow = useMutation({
		mutationFn: (id) => notificationsAPI.runScheduledReportNow(id),
		onSuccess: () => {
			invalidate();
			toast.success("Report scheduled for immediate delivery");
		},
		onError: (err) => toast.error(err.response?.data?.error || "Failed to run"),
	});

	const previewReport = async (r) => {
		setPreviewingId(r.id);
		try {
			const res = await notificationsAPI.previewScheduledReport(r.id);
			const blob = new Blob([res.data], { type: "application/pdf" });
			const disposition = res.headers?.["content-disposition"] || "";
			const match = /filename="?([^";]+)"?/.exec(disposition);
			const filename = match ? match[1] : `report-${r.id}.pdf`;
			const url = window.URL.createObjectURL(blob);
			const a = document.createElement("a");
			a.href = url;
			a.download = filename;
			document.body.appendChild(a);
			a.click();
			a.remove();
			window.URL.revokeObjectURL(url);
			if (res.headers?.["x-report-logo"] === "default") {
				toast.info(
					"The PDF uses the default logo. Upload a PNG or JPEG under Settings → Branding for your own logo.",
				);
			}
		} catch (err) {
			let message = "Preview failed";
			const data = err.response?.data;
			if (data instanceof Blob) {
				try {
					message = JSON.parse(await data.text()).error || message;
				} catch {
					/* not JSON */
				}
			} else if (data?.error) {
				message = data.error;
			}
			toast.error(message);
		} finally {
			setPreviewingId(null);
		}
	};

	const sendTest = (id) => {
		testNotify.mutate(id, {
			onSuccess: () => {
				toast.info("Test notification enqueued");
				setTimeout(
					() =>
						queryClient.invalidateQueries({
							queryKey: ["notifications", "delivery-log"],
						}),
					3000,
				);
			},
			onError: (err) =>
				toast.error(err.response?.data?.error || err.message || "Test failed"),
		});
	};

	const openEditDest = async (d) => {
		let loadedConfig = {};
		if (d.has_secret) {
			try {
				const resp = await notificationsAPI.getDestinationConfig(d.id);
				loadedConfig = resp.data;
			} catch {
				/* fallback to empty */
			}
		}
		setDestModal({
			open: true,
			editing: { ...d, _loadedConfig: loadedConfig },
		});
	};

	const handleDestSave = (data) => {
		if (destModal.editing) {
			updateDest.mutate({ id: destModal.editing.id, body: data });
		} else {
			createDest.mutate(data);
		}
	};

	const handleRouteSave = (data) => {
		if (routeModal.editing) {
			updateRoute.mutate({ id: routeModal.editing.id, body: data });
		} else {
			createRoute.mutate(data);
		}
	};

	const handleReportSave = (data) => {
		if (reportModal.editing) {
			updateReport.mutate({ id: reportModal.editing.id, body: data });
		} else {
			createReport.mutate(data);
		}
	};

	const TH =
		"px-4 py-2 text-left text-xs font-medium text-secondary-500 dark:text-white uppercase tracking-wider";
	const TD =
		"px-4 py-2 text-sm text-secondary-900 dark:text-white whitespace-nowrap";
	const TDW = "px-4 py-2 text-sm text-secondary-900 dark:text-white";
	const W_STATUS = "w-24";
	const W_ACTIONS = "w-36";

	// When used as a standalone page, render all sections. When panel prop is set, render only that section.
	const showAll = !panel;
	const showDest = showAll || panel === "destinations";
	const showRoutes = showAll || panel === "routes";
	const showReports = showAll || panel === "reports";
	const showLog = showAll || panel === "log";

	return (
		<div className="space-y-6">
			{/* Header - only shown on standalone page */}
			{showAll && (
				<div className="flex items-center justify-between">
					<div>
						<h1 className="text-2xl font-semibold text-secondary-900 dark:text-white">
							Notifications
						</h1>
						<p className="text-sm text-secondary-600 dark:text-white mt-1">
							Manage destinations, routing rules, scheduled reports, and
							delivery history
						</p>
					</div>
					{canLog && (
						<button
							type="button"
							onClick={() =>
								queryClient.invalidateQueries({
									queryKey: ["notifications", "delivery-log"],
								})
							}
							className="btn-outline flex items-center gap-2"
						>
							<RefreshCw className="h-4 w-4" /> Refresh log
						</button>
					)}
				</div>
			)}

			{/* ── Destinations ── */}
			{showDest && canManage && (
				<div className="card p-4 md:p-6 space-y-4">
					<div className="flex items-center justify-between">
						<h2 className="text-lg font-semibold text-secondary-900 dark:text-white">
							Destinations
						</h2>
						<button
							type="button"
							className="btn-primary flex items-center gap-2"
							onClick={() => setDestModal({ open: true, editing: null })}
						>
							<Plus className="h-4 w-4" /> Add destination
						</button>
					</div>

					{destLoading && <Loader2 className="h-5 w-5 animate-spin mx-auto" />}

					{!destLoading && destinations.length === 0 && (
						<div className="rounded-md p-4 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 text-center">
							<p className="text-sm text-blue-800 dark:text-blue-200">
								No destinations yet. Add one to start receiving notifications.
							</p>
						</div>
					)}

					{destinations.length > 0 && (
						<div className="overflow-x-auto">
							<table className="min-w-full table-fixed divide-y divide-secondary-200 dark:divide-secondary-600">
								<thead className="bg-secondary-50 dark:bg-secondary-700">
									<tr>
										<th className={`${TH} w-28`}>Channel</th>
										<th className={TH}>Name</th>
										<th className={`${TH} w-20`}>Enabled</th>
										<th className={`${TH} ${W_ACTIONS}`}>Actions</th>
									</tr>
								</thead>
								<tbody className="bg-white dark:bg-secondary-800 divide-y divide-secondary-200 dark:divide-secondary-600">
									{destinations.map((d) => {
										const isBuiltIn = d.id === "internal-alerts";
										return (
											<tr
												key={d.id}
												className="hover:bg-secondary-50 dark:hover:bg-secondary-700"
											>
												<td className={TD}>
													<span className="inline-flex items-center gap-2">
														{channelIcon(d.channel_type)}
														{isBuiltIn ? (
															<span className="text-xs text-secondary-500">
																Built-in
															</span>
														) : (
															d.channel_type
														)}
													</span>
												</td>
												<td className={TD}>{d.display_name}</td>
												<td className={TD}>
													<button
														type="button"
														onClick={() =>
															updateDest.mutate({
																id: d.id,
																body: { enabled: !d.enabled },
															})
														}
														disabled={updateDest.isPending}
														className={`relative inline-flex h-5 w-9 items-center rounded-md transition-colors focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2 ${
															d.enabled
																? "bg-primary-600 dark:bg-primary-500"
																: "bg-secondary-200 dark:bg-secondary-600"
														} disabled:opacity-50`}
													>
														<span
															className={`inline-block h-3 w-3 transform rounded-md bg-white transition-transform ${d.enabled ? "translate-x-5" : "translate-x-1"}`}
														/>
													</button>
												</td>
												<td className={`${TD} flex items-center gap-2`}>
													{!isBuiltIn && (
														<button
															type="button"
															className="text-primary-600 hover:text-primary-700 inline-flex items-center gap-1 text-xs"
															onClick={() => sendTest(d.id)}
															disabled={testNotify.isPending}
														>
															<Send className="h-3.5 w-3.5" /> Test
														</button>
													)}
													<button
														type="button"
														className="text-primary-600 hover:text-primary-700 inline-flex items-center gap-1 text-xs"
														onClick={() => openEditDest(d)}
													>
														<Edit2 className="h-3.5 w-3.5" /> Edit
													</button>
													{!isBuiltIn && (
														<button
															type="button"
															className="text-red-600 hover:text-red-700 inline-flex items-center gap-1 text-xs"
															onClick={() => {
																if (confirm("Delete this destination?"))
																	deleteDest.mutate(d.id);
															}}
														>
															<Trash2 className="h-3.5 w-3.5" />
														</button>
													)}
												</td>
											</tr>
										);
									})}
								</tbody>
							</table>
						</div>
					)}
				</div>
			)}

			{/* ── Event Rules ── */}
			{showRoutes && canManage && (
				<div className="card p-4 md:p-6 space-y-4">
					<div className="flex items-center justify-between">
						<h2 className="text-lg font-semibold text-secondary-900 dark:text-white">
							Event Rules
						</h2>
						<button
							type="button"
							className="btn-primary flex items-center gap-2"
							onClick={() => setRouteModal({ open: true, editing: null })}
							disabled={destinations.length === 0}
						>
							<Plus className="h-4 w-4" /> Add event rule
						</button>
					</div>

					{routesLoading && (
						<Loader2 className="h-5 w-5 animate-spin mx-auto" />
					)}

					{!routesLoading && routes.length === 0 && destinations.length > 0 && (
						<div className="rounded-md p-4 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 text-center">
							<p className="text-sm text-blue-800 dark:text-blue-200">
								No event rules yet. Event rules map events to destinations.
							</p>
						</div>
					)}

					{routes.length > 0 && (
						<div className="overflow-x-auto">
							<table className="min-w-full table-fixed divide-y divide-secondary-200 dark:divide-secondary-600">
								<thead className="bg-secondary-50 dark:bg-secondary-700">
									<tr>
										<th className={TH}>Destination</th>
										<th className={TH}>Events</th>
										<th className={`${TH} w-32`}>Min severity</th>
										<th className={TH}>Scope</th>
										<th className={`${TH} ${W_STATUS}`}>Status</th>
										<th className={`${TH} ${W_ACTIONS}`}>Actions</th>
									</tr>
								</thead>
								<tbody className="bg-white dark:bg-secondary-800 divide-y divide-secondary-200 dark:divide-secondary-600">
									{routes.map((row) => (
										<tr
											key={row.id}
											className="hover:bg-secondary-50 dark:hover:bg-secondary-700"
										>
											<td className={TD}>
												{row.destination_display_name || row.destination_id}
											</td>
											<td className={TDW}>
												{Array.isArray(row.event_types) &&
												row.event_types.includes("*")
													? "All events"
													: Array.isArray(row.event_types)
														? row.event_types
																.map((e) => e.replace(/_/g, " "))
																.join(", ")
														: "All events"}
											</td>
											<td className={TD}>
												<span
													className={`px-2 py-0.5 text-xs font-medium rounded-md ${
														row.min_severity === "critical"
															? "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200"
															: row.min_severity === "error"
																? "bg-orange-100 text-orange-800 dark:bg-orange-900 dark:text-orange-200"
																: row.min_severity === "warning"
																	? "bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200"
																	: "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200"
													}`}
												>
													{row.min_severity}
												</span>
											</td>
											<td className={TDW}>
												{Array.isArray(row.host_group_ids) &&
												row.host_group_ids.length > 0
													? row.host_group_ids
															.map((id) => hostGroupNameMap[id] || id)
															.join(", ")
													: "All"}
											</td>
											<td className={TD}>
												<span
													className={`px-2 py-0.5 text-xs font-medium rounded-md ${row.enabled ? "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200" : "bg-secondary-100 text-secondary-600 dark:bg-secondary-700 dark:text-secondary-300"}`}
												>
													{row.enabled ? "Active" : "Disabled"}
												</span>
											</td>
											<td className={`${TD} flex items-center gap-2`}>
												<button
													type="button"
													className="text-primary-600 hover:text-primary-700 inline-flex items-center gap-1 text-xs"
													onClick={() =>
														setRouteModal({ open: true, editing: row })
													}
												>
													<Edit2 className="h-3.5 w-3.5" /> Edit
												</button>
												<button
													type="button"
													className="text-red-600 hover:text-red-700 inline-flex items-center gap-1 text-xs"
													onClick={() => {
														if (confirm("Delete this route?"))
															deleteRoute.mutate(row.id);
													}}
												>
													<Trash2 className="h-3.5 w-3.5" />
												</button>
											</td>
										</tr>
									))}
								</tbody>
							</table>
						</div>
					)}
				</div>
			)}

			{/* ── Scheduled Reports ── */}
			{showReports && canManage && (
				<div className="card p-4 md:p-6 space-y-4">
					<div className="flex items-center justify-between">
						<h2 className="text-lg font-semibold text-secondary-900 dark:text-white">
							Scheduled reports
						</h2>
						<button
							type="button"
							className="btn-primary flex items-center gap-2"
							onClick={() => setReportModal({ open: true, editing: null })}
							disabled={destinations.length === 0}
						>
							<Plus className="h-4 w-4" /> New report
						</button>
					</div>

					{reportsLoading && (
						<Loader2 className="h-5 w-5 animate-spin mx-auto" />
					)}

					{!reportsLoading && scheduledReports.length === 0 && (
						<div className="rounded-md p-4 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 text-center">
							<p className="text-sm text-blue-800 dark:text-blue-200">
								No scheduled reports yet.
							</p>
						</div>
					)}

					{scheduledReports.length > 0 && (
						<div className="overflow-x-auto">
							<table className="min-w-full table-fixed divide-y divide-secondary-200 dark:divide-secondary-600">
								<thead className="bg-secondary-50 dark:bg-secondary-700">
									<tr>
										<th className={`${TH} w-10`} />
										<th className={TH}>Name</th>
										<th className={TH}>Schedule</th>
										<th className={TH}>Next run</th>
										<th className={`${TH} ${W_STATUS}`}>Status</th>
										<th className={`${TH} ${W_ACTIONS}`}>Actions</th>
									</tr>
								</thead>
								<tbody className="bg-white dark:bg-secondary-800 divide-y divide-secondary-200 dark:divide-secondary-600">
									{scheduledReports.map((r) => (
										<tr
											key={r.id}
											className="hover:bg-secondary-50 dark:hover:bg-secondary-700"
										>
											<td className="px-2 py-2">
												<button
													type="button"
													className="inline-flex items-center justify-center w-6 h-6 rounded border border-transparent text-white bg-green-600 hover:bg-green-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-green-500 disabled:opacity-40"
													onClick={() => runReportNow.mutate(r.id)}
													disabled={runReportNow.isPending || !r.enabled}
													title={
														!r.enabled ? "Enable the report first" : "Run now"
													}
												>
													<Play className="h-3.5 w-3.5" />
												</button>
											</td>
											<td className={TD}>{r.name}</td>
											<td className={TD}>
												<span className="flex items-center gap-1">
													<Clock className="h-3.5 w-3.5 text-secondary-400" />
													{describeSchedule(r.cron_expr)}
												</span>
											</td>
											<td className={TD}>
												{r.next_run_at
													? formatRelativeTime(r.next_run_at)
													: " -"}
											</td>
											<td className={TD}>
												<span
													className={`px-2 py-0.5 text-xs font-medium rounded-md ${r.enabled ? "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200" : "bg-secondary-100 text-secondary-600 dark:bg-secondary-700 dark:text-secondary-300"}`}
												>
													{r.enabled ? "Active" : "Disabled"}
												</span>
											</td>
											<td className={`${TD} flex items-center gap-2`}>
												<button
													type="button"
													className="text-primary-600 hover:text-primary-700 inline-flex items-center gap-1 text-xs"
													onClick={() =>
														setReportModal({ open: true, editing: r })
													}
												>
													<Edit2 className="h-3.5 w-3.5" /> Edit
												</button>
												<button
													type="button"
													className="text-primary-600 hover:text-primary-700 inline-flex items-center gap-1 text-xs disabled:opacity-40"
													onClick={() => previewReport(r)}
													disabled={previewingId === r.id}
													title="Download this report as PDF (nothing is sent)"
												>
													<FileText className="h-3.5 w-3.5" />{" "}
													{previewingId === r.id ? "Rendering…" : "Preview"}
												</button>
												<button
													type="button"
													className="text-red-600 hover:text-red-700 inline-flex items-center gap-1 text-xs"
													onClick={() => {
														if (confirm("Delete this report?"))
															deleteReport.mutate(r.id);
													}}
												>
													<Trash2 className="h-3.5 w-3.5" />
												</button>
											</td>
										</tr>
									))}
								</tbody>
							</table>
						</div>
					)}
				</div>
			)}

			{/* ── Delivery Log ── */}
			{showLog && canLog && (
				<div className="card p-4 md:p-6 space-y-4">
					<div className="flex items-center justify-between">
						<h2 className="text-lg font-semibold text-secondary-900 dark:text-white">
							Delivery log
						</h2>
						{logLoading && <Loader2 className="h-5 w-5 animate-spin" />}
					</div>

					{!logLoading && deliveryLog.length === 0 ? (
						<p className="text-sm text-secondary-500">
							No delivery entries yet.
						</p>
					) : (
						<>
							<div className="overflow-x-auto">
								<table className="min-w-full table-fixed divide-y divide-secondary-200 dark:divide-secondary-600">
									<thead className="bg-secondary-50 dark:bg-secondary-700">
										<tr>
											<th className={`${TH} w-28`}>Time</th>
											<th className={`${TH} ${W_STATUS}`}>Status</th>
											<th className={TH}>Event</th>
											<th className={TH}>Destination</th>
											<th className={TH}>Reference</th>
											<th className={TH}>Error</th>
										</tr>
									</thead>
									<tbody className="bg-white dark:bg-secondary-800 divide-y divide-secondary-200 dark:divide-secondary-600">
										{deliveryLog.map((row) => (
											<tr
												key={row.id}
												className="hover:bg-secondary-50 dark:hover:bg-secondary-700 align-top"
											>
												<td className={TD} title={row.created_at || ""}>
													{row.created_at
														? formatRelativeTime(row.created_at)
														: " -"}
												</td>
												<td className="px-4 py-2">{statusBadge(row.status)}</td>
												<td className={TD}>{row.event_type}</td>
												<td className={TD}>
													{destNameMap[row.destination_id] ||
														row.destination_id}
												</td>
												<td className={TDW}>
													{(() => {
														const rid =
															typeof row.reference_id === "string"
																? row.reference_id
																: "";
														const rt = row.reference_type;
														let href = null;
														if (rid) {
															if (rt === "patch_run")
																href = `/patching/runs/${rid}`;
															else if (
																rt === "host" &&
																row.event_type === "compliance_scan_completed"
															)
																href = `/compliance/hosts/${rid}`;
															else if (rt === "host") href = `/hosts/${rid}`;
															else if (rt === "alert") href = `/hosts/${rid}`;
														}
														return href ? (
															<Link
																to={href}
																className="text-primary-600 hover:text-primary-700 hover:underline"
															>
																{rt}:{rid}
															</Link>
														) : (
															<span>
																{rt}:{rid || " -"}
															</span>
														);
													})()}
												</td>
												<td className="px-4 py-2 text-sm text-red-600 dark:text-red-400 max-w-xs break-words whitespace-normal">
													{row.error_message || " -"}
												</td>
											</tr>
										))}
									</tbody>
								</table>
							</div>
							<div className="flex items-center justify-between pt-2">
								<p className="text-xs text-secondary-500">
									Page {logPage + 1}
									{deliveryLog.length < logPageSize && logPage === 0
										? ` · ${deliveryLog.length} entries`
										: ""}
								</p>
								<div className="flex gap-2">
									<button
										type="button"
										className="p-1.5 rounded-lg bg-secondary-100 dark:bg-secondary-700 disabled:opacity-50"
										disabled={logPage === 0}
										onClick={() => setLogPage((p) => Math.max(0, p - 1))}
									>
										<ChevronLeft className="h-4 w-4" />
									</button>
									<button
										type="button"
										className="p-1.5 rounded-lg bg-secondary-100 dark:bg-secondary-700 disabled:opacity-50"
										disabled={deliveryLog.length < logPageSize}
										onClick={() => setLogPage((p) => p + 1)}
									>
										<ChevronRight className="h-4 w-4" />
									</button>
								</div>
							</div>
						</>
					)}
				</div>
			)}

			{showAll && !canManage && !canLog && (
				<div className="card p-8 text-center text-secondary-600">
					You don&apos;t have permission to view this page.
				</div>
			)}

			{/* Modals */}
			<DestinationModal
				key={destModal.editing?.id || "new-dest"}
				isOpen={destModal.open}
				onClose={() => setDestModal({ open: false, editing: null })}
				onSave={handleDestSave}
				editingDest={destModal.editing}
				isPending={createDest.isPending || updateDest.isPending}
			/>
			<RouteModal
				key={routeModal.editing?.id || "new-route"}
				isOpen={routeModal.open}
				onClose={() => setRouteModal({ open: false, editing: null })}
				onSave={handleRouteSave}
				editingRoute={routeModal.editing}
				destinations={destinations}
				hostGroups={hostGroupOptions}
				hosts={Array.isArray(hostsList) ? hostsList : []}
				isPending={createRoute.isPending || updateRoute.isPending}
			/>
			<ReportModal
				key={reportModal.editing?.id || "new-report"}
				isOpen={reportModal.open}
				onClose={() => setReportModal({ open: false, editing: null })}
				onSave={handleReportSave}
				editingReport={reportModal.editing}
				destinations={destinations}
				hostGroups={hostGroupOptions}
				isPending={createReport.isPending || updateReport.isPending}
			/>
		</div>
	);
};

// Default export for standalone settings page (renders all panels)
const AlertChannels = () => <NotificationPanel />;
export default AlertChannels;
