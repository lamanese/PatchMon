import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
	AlertTriangle,
	CalendarClock,
	Edit,
	Plus,
	Trash2,
	Wrench,
} from "lucide-react";
import { useId, useState } from "react";
import { useToast } from "../contexts/ToastContext";
import { formatDate, hostGroupsAPI, patchSchedulesAPI } from "../utils/api";

const WEEKDAYS = [
	"Sunday",
	"Monday",
	"Tuesday",
	"Wednesday",
	"Thursday",
	"Friday",
	"Saturday",
];

const browserTimezone = () => {
	try {
		return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
	} catch {
		return "UTC";
	}
};

const timezoneOptions = () => {
	try {
		return Intl.supportedValuesOf("timeZone");
	} catch {
		return ["UTC"];
	}
};

// Local datetime-local input value -> RFC3339 (UTC) for the API.
const toRFC3339 = (local) => {
	const d = new Date(local);
	return Number.isNaN(d.getTime()) ? "" : d.toISOString();
};

// RFC3339 -> datetime-local input value in the browser's timezone.
const toLocalInput = (iso) => {
	if (!iso) return "";
	const d = new Date(iso);
	if (Number.isNaN(d.getTime())) return "";
	const pad = (n) => String(n).padStart(2, "0");
	return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
};

const describeSchedule = (s) => {
	if (s.schedule_type === "once") {
		return `Once at ${formatDate(s.run_at)}`;
	}
	if (s.schedule_type === "daily") {
		return `Every day at ${s.time_of_day} (${s.timezone})`;
	}
	return `Every ${WEEKDAYS[s.weekday] ?? "?"} ${s.time_of_day} (${s.timezone})`;
};

const PatchSchedules = () => {
	const toast = useToast();
	const queryClient = useQueryClient();
	const [showFormModal, setShowFormModal] = useState(false);
	const [editingSchedule, setEditingSchedule] = useState(null);
	const [scheduleToDelete, setScheduleToDelete] = useState(null);

	const {
		data: schedules,
		isLoading,
		error,
	} = useQuery({
		queryKey: ["patchSchedules"],
		queryFn: () => patchSchedulesAPI.list().then((res) => res.data),
		refetchInterval: 60000,
	});

	const { data: hostGroups } = useQuery({
		queryKey: ["hostGroups"],
		queryFn: () => hostGroupsAPI.list().then((res) => res.data),
	});

	const invalidate = () =>
		queryClient.invalidateQueries({ queryKey: ["patchSchedules"] });

	const apiError = (err, fallback) =>
		err?.response?.data?.error || err?.message || fallback;

	const createMutation = useMutation({
		mutationFn: (data) => patchSchedulesAPI.create(data),
		onSuccess: () => {
			invalidate();
			setShowFormModal(false);
			toast.success("Patch schedule created");
		},
		onError: (err) => toast.error(apiError(err, "Failed to create schedule")),
	});

	const updateMutation = useMutation({
		mutationFn: ({ id, data }) => patchSchedulesAPI.update(id, data),
		onSuccess: () => {
			invalidate();
			setShowFormModal(false);
			setEditingSchedule(null);
			toast.success("Patch schedule updated");
		},
		onError: (err) => toast.error(apiError(err, "Failed to update schedule")),
	});

	const toggleMutation = useMutation({
		mutationFn: ({ id, enabled }) => patchSchedulesAPI.update(id, { enabled }),
		onSuccess: invalidate,
		onError: (err) => toast.error(apiError(err, "Failed to toggle schedule")),
	});

	const deleteMutation = useMutation({
		mutationFn: (id) => patchSchedulesAPI.delete(id),
		onSuccess: () => {
			invalidate();
			setScheduleToDelete(null);
			toast.success("Patch schedule deleted");
		},
		onError: (err) => toast.error(apiError(err, "Failed to delete schedule")),
	});

	const openCreate = () => {
		setEditingSchedule(null);
		setShowFormModal(true);
	};

	const openEdit = (schedule) => {
		setEditingSchedule(schedule);
		setShowFormModal(true);
	};

	const handleSubmit = (data) => {
		if (editingSchedule) {
			updateMutation.mutate({ id: editingSchedule.id, data });
		} else {
			createMutation.mutate(data);
		}
	};

	return (
		<div className="space-y-6">
			<div className="flex items-start justify-between gap-4">
				<p className="text-sm text-secondary-500 dark:text-white max-w-3xl">
					Scheduled patch runs target one host group and upgrade all packages on
					every host in the group at the configured time. Missed one-time
					schedules expire instead of running late.
				</p>
				<button
					type="button"
					onClick={openCreate}
					className="btn-primary flex items-center gap-2 flex-shrink-0"
					title="Create patch schedule"
				>
					<Plus className="h-4 w-4" />
					Create Schedule
				</button>
			</div>

			<div className="bg-white dark:bg-secondary-800 shadow overflow-hidden sm:rounded-lg">
				{schedules && schedules.length > 0 ? (
					<div className="overflow-x-auto">
						<table className="min-w-full divide-y divide-secondary-200 dark:divide-secondary-600">
							<thead className="bg-secondary-50 dark:bg-secondary-700">
								<tr>
									<th className="px-6 py-3 text-left text-xs font-medium text-secondary-500 dark:text-white uppercase tracking-wider">
										Name
									</th>
									<th className="px-6 py-3 text-left text-xs font-medium text-secondary-500 dark:text-white uppercase tracking-wider">
										Host Group
									</th>
									<th className="px-6 py-3 text-left text-xs font-medium text-secondary-500 dark:text-white uppercase tracking-wider">
										Schedule
									</th>
									<th className="px-6 py-3 text-left text-xs font-medium text-secondary-500 dark:text-white uppercase tracking-wider">
										Last run
									</th>
									<th className="px-6 py-3 text-left text-xs font-medium text-secondary-500 dark:text-white uppercase tracking-wider">
										Enabled
									</th>
									<th className="px-6 py-3 text-right text-xs font-medium text-secondary-500 dark:text-white uppercase tracking-wider">
										Actions
									</th>
								</tr>
							</thead>
							<tbody className="bg-white dark:bg-secondary-800 divide-y divide-secondary-200 dark:divide-secondary-600">
								{schedules.map((s) => (
									<tr
										key={s.id}
										className="hover:bg-secondary-50 dark:hover:bg-secondary-700"
									>
										<td className="px-6 py-4 whitespace-nowrap">
											<div className="text-sm font-medium text-secondary-900 dark:text-white">
												{s.name}
											</div>
										</td>
										<td className="px-6 py-4 whitespace-nowrap text-sm text-secondary-500 dark:text-white">
											{s.host_group_name}
										</td>
										<td className="px-6 py-4 whitespace-nowrap text-sm text-secondary-500 dark:text-white">
											<div className="flex items-center gap-2">
												<CalendarClock className="h-4 w-4 flex-shrink-0" />
												{describeSchedule(s)}
											</div>
											{s.missed_at && (
												<span className="mt-1 inline-flex px-2 py-0.5 text-xs font-medium rounded-md bg-amber-100 text-amber-800 dark:bg-amber-900 dark:text-amber-200">
													Missed at {formatDate(s.missed_at)}
												</span>
											)}
										</td>
										<td className="px-6 py-4 whitespace-nowrap text-sm text-secondary-500 dark:text-white">
											{s.last_run_at ? formatDate(s.last_run_at) : "Never"}
										</td>
										<td className="px-6 py-4 whitespace-nowrap">
											<button
												type="button"
												role="switch"
												aria-checked={s.enabled}
												onClick={() =>
													toggleMutation.mutate({
														id: s.id,
														enabled: !s.enabled,
													})
												}
												disabled={toggleMutation.isPending}
												className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors ${
													s.enabled
														? "bg-primary-600"
														: "bg-secondary-300 dark:bg-secondary-600"
												}`}
												title={
													s.enabled ? "Disable schedule" : "Enable schedule"
												}
											>
												<span
													className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
														s.enabled ? "translate-x-4" : "translate-x-1"
													}`}
												/>
											</button>
										</td>
										<td className="px-6 py-4 whitespace-nowrap text-right text-sm font-medium">
											<div className="flex items-center justify-end gap-3">
												<button
													type="button"
													onClick={() => openEdit(s)}
													className="text-secondary-400 hover:text-secondary-600 dark:text-white dark:hover:text-secondary-300"
													title="Edit schedule"
												>
													<Edit className="h-4 w-4" />
												</button>
												<button
													type="button"
													onClick={() => setScheduleToDelete(s)}
													className="text-secondary-400 hover:text-danger-600 dark:text-white dark:hover:text-danger-400"
													title="Delete schedule"
												>
													<Trash2 className="h-4 w-4" />
												</button>
											</div>
										</td>
									</tr>
								))}
							</tbody>
						</table>
					</div>
				) : isLoading ? (
					<div className="p-12 text-center">
						<div className="flex items-center justify-center">
							<div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary-600"></div>
						</div>
					</div>
				) : error ? (
					<div className="p-6">
						<div className="bg-danger-50 dark:bg-danger-900 border border-danger-200 dark:border-danger-700 rounded-md p-4">
							<div className="flex">
								<AlertTriangle className="h-5 w-5 text-danger-400 dark:text-danger-300" />
								<div className="ml-3">
									<h3 className="text-sm font-medium text-danger-800 dark:text-danger-200">
										Error loading patch schedules
									</h3>
									<p className="text-sm text-danger-700 dark:text-danger-300 mt-1">
										{error.message || "Failed to load patch schedules"}
									</p>
								</div>
							</div>
						</div>
					</div>
				) : (
					<div className="p-12 text-center">
						<Wrench className="h-12 w-12 text-secondary-400 mx-auto mb-4" />
						<p className="text-secondary-500 dark:text-white">
							No patch schedules found
						</p>
						<p className="text-sm text-secondary-400 dark:text-white mt-2">
							Click "Create Schedule" to schedule a patch run for a host group
						</p>
					</div>
				)}
			</div>

			{showFormModal && (
				<PatchScheduleFormModal
					schedule={editingSchedule}
					hostGroups={hostGroups || []}
					onClose={() => {
						setShowFormModal(false);
						setEditingSchedule(null);
					}}
					onSubmit={handleSubmit}
					isLoading={createMutation.isPending || updateMutation.isPending}
				/>
			)}

			{scheduleToDelete && (
				<DeletePatchScheduleModal
					schedule={scheduleToDelete}
					onClose={() => setScheduleToDelete(null)}
					onConfirm={() => deleteMutation.mutate(scheduleToDelete.id)}
					isLoading={deleteMutation.isPending}
				/>
			)}
		</div>
	);
};

const PatchScheduleFormModal = ({
	schedule,
	hostGroups,
	onClose,
	onSubmit,
	isLoading,
}) => {
	const nameId = useId();
	const groupId = useId();
	const typeId = useId();
	const runAtId = useId();
	const weekdayId = useId();
	const timeOfDayId = useId();
	const timezoneId = useId();
	const enabledId = useId();

	const [form, setForm] = useState(() => ({
		name: schedule?.name || "",
		host_group_id: schedule?.host_group_id || "",
		schedule_type: schedule?.schedule_type || "weekly",
		run_at_local: toLocalInput(schedule?.run_at),
		weekday: schedule?.weekday ?? 0,
		time_of_day: schedule?.time_of_day || "03:00",
		timezone: schedule?.timezone || browserTimezone(),
		enabled: schedule?.enabled ?? true,
	}));

	const set = (field, value) => setForm((f) => ({ ...f, [field]: value }));

	// Preview which hosts the schedule would affect, so arming a patch run is
	// never blind. Best effort: without can_view_hosts the preview is hidden.
	const { data: groupHosts } = useQuery({
		queryKey: ["hostGroupHosts", form.host_group_id],
		queryFn: () =>
			hostGroupsAPI.getHosts(form.host_group_id).then((res) => res.data),
		enabled: !!form.host_group_id,
		retry: false,
	});

	const handleSubmit = (e) => {
		e.preventDefault();
		const data = {
			name: form.name,
			host_group_id: form.host_group_id,
			schedule_type: form.schedule_type,
			enabled: form.enabled,
		};
		if (form.schedule_type === "once") {
			data.run_at = toRFC3339(form.run_at_local);
			data.timezone = "UTC";
		} else {
			data.time_of_day = form.time_of_day;
			data.timezone = form.timezone;
			if (form.schedule_type === "weekly") {
				data.weekday = Number(form.weekday);
			}
		}
		onSubmit(data);
	};

	return (
		<div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
			<div className="bg-white dark:bg-secondary-800 rounded-lg p-6 w-full max-w-md max-h-[90vh] overflow-y-auto">
				<h3 className="text-lg font-semibold text-secondary-900 dark:text-white mb-4">
					{schedule ? "Edit Patch Schedule" : "Create Patch Schedule"}
				</h3>

				<form onSubmit={handleSubmit} className="space-y-4">
					<div>
						<label
							htmlFor={nameId}
							className="block text-sm font-medium text-secondary-700 dark:text-secondary-200 mb-1"
						>
							Name *
						</label>
						<input
							type="text"
							id={nameId}
							value={form.name}
							onChange={(e) => set("name", e.target.value)}
							required
							maxLength={200}
							className="w-full px-3 py-2 border border-secondary-300 dark:border-secondary-600 rounded-md focus:outline-none focus:ring-2 focus:ring-primary-500 bg-white dark:bg-secondary-700 text-secondary-900 dark:text-white placeholder-secondary-500 dark:placeholder-secondary-400"
							placeholder="e.g., Weekly server patching"
						/>
					</div>

					<div>
						<label
							htmlFor={groupId}
							className="block text-sm font-medium text-secondary-700 dark:text-secondary-200 mb-1"
						>
							Host Group *
						</label>
						<select
							id={groupId}
							value={form.host_group_id}
							onChange={(e) => set("host_group_id", e.target.value)}
							required
							className="w-full px-3 py-2 border border-secondary-300 dark:border-secondary-600 rounded-md focus:outline-none focus:ring-2 focus:ring-primary-500 bg-white dark:bg-secondary-700 text-secondary-900 dark:text-white"
						>
							<option value="">Select a host group…</option>
							{hostGroups.map((g) => (
								<option key={g.id} value={g.id}>
									{g.name}
								</option>
							))}
						</select>
						{Array.isArray(groupHosts) && (
							<div className="mt-2 text-xs text-secondary-500 dark:text-secondary-300">
								<p>
									{groupHosts.length} host{groupHosts.length !== 1 ? "s" : ""}{" "}
									in this group will be patched.
								</p>
								{groupHosts.length > 0 && (
									<ul className="mt-1 max-h-28 overflow-y-auto border border-secondary-200 dark:border-secondary-600 rounded-md divide-y divide-secondary-100 dark:divide-secondary-700">
										{groupHosts.map((h) => (
											<li
												key={h.id}
												className="flex items-center justify-between px-2 py-1"
											>
												<span className="truncate">
													{h.hostname || h.friendly_name}
												</span>
												<span className="ml-2 flex-shrink-0 px-1.5 py-0.5 rounded text-[10px] font-medium bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200">
													will patch
												</span>
											</li>
										))}
									</ul>
								)}
							</div>
						)}
					</div>

					<div>
						<label
							htmlFor={typeId}
							className="block text-sm font-medium text-secondary-700 dark:text-secondary-200 mb-1"
						>
							Schedule Type
						</label>
						<select
							id={typeId}
							value={form.schedule_type}
							onChange={(e) => set("schedule_type", e.target.value)}
							className="w-full px-3 py-2 border border-secondary-300 dark:border-secondary-600 rounded-md focus:outline-none focus:ring-2 focus:ring-primary-500 bg-white dark:bg-secondary-700 text-secondary-900 dark:text-white"
						>
							<option value="weekly">Weekly (recurring)</option>
							<option value="daily">Daily (recurring)</option>
							<option value="once">Once (specific date and time)</option>
						</select>
					</div>

					{form.schedule_type === "once" ? (
						<div>
							<label
								htmlFor={runAtId}
								className="block text-sm font-medium text-secondary-700 dark:text-secondary-200 mb-1"
							>
								Run at (your local time) *
							</label>
							<input
								type="datetime-local"
								id={runAtId}
								value={form.run_at_local}
								onChange={(e) => set("run_at_local", e.target.value)}
								required
								className="w-full px-3 py-2 border border-secondary-300 dark:border-secondary-600 rounded-md focus:outline-none focus:ring-2 focus:ring-primary-500 bg-white dark:bg-secondary-700 text-secondary-900 dark:text-white"
							/>
						</div>
					) : (
						<>
							{form.schedule_type === "weekly" && (
								<div>
									<label
										htmlFor={weekdayId}
										className="block text-sm font-medium text-secondary-700 dark:text-secondary-200 mb-1"
									>
										Weekday *
									</label>
									<select
										id={weekdayId}
										value={form.weekday}
										onChange={(e) => set("weekday", e.target.value)}
										className="w-full px-3 py-2 border border-secondary-300 dark:border-secondary-600 rounded-md focus:outline-none focus:ring-2 focus:ring-primary-500 bg-white dark:bg-secondary-700 text-secondary-900 dark:text-white"
									>
										{WEEKDAYS.map((d, i) => (
											<option key={d} value={i}>
												{d}
											</option>
										))}
									</select>
								</div>
							)}
							<div>
								<label
									htmlFor={timeOfDayId}
									className="block text-sm font-medium text-secondary-700 dark:text-secondary-200 mb-1"
								>
									Time *
								</label>
								<input
									type="time"
									id={timeOfDayId}
									value={form.time_of_day}
									onChange={(e) => set("time_of_day", e.target.value)}
									required
									className="w-full px-3 py-2 border border-secondary-300 dark:border-secondary-600 rounded-md focus:outline-none focus:ring-2 focus:ring-primary-500 bg-white dark:bg-secondary-700 text-secondary-900 dark:text-white"
								/>
							</div>
							<div>
								<label
									htmlFor={timezoneId}
									className="block text-sm font-medium text-secondary-700 dark:text-secondary-200 mb-1"
								>
									Timezone
								</label>
								<select
									id={timezoneId}
									value={form.timezone}
									onChange={(e) => set("timezone", e.target.value)}
									className="w-full px-3 py-2 border border-secondary-300 dark:border-secondary-600 rounded-md focus:outline-none focus:ring-2 focus:ring-primary-500 bg-white dark:bg-secondary-700 text-secondary-900 dark:text-white"
								>
									{timezoneOptions().map((tz) => (
										<option key={tz} value={tz}>
											{tz}
										</option>
									))}
								</select>
								<p className="text-xs text-secondary-400 dark:text-secondary-300 mt-1">
									The time is evaluated in this timezone, so it stays at the
									same local time across DST changes.
								</p>
							</div>
						</>
					)}

					<div className="flex items-center gap-2">
						<input
							type="checkbox"
							id={enabledId}
							checked={form.enabled}
							onChange={(e) => set("enabled", e.target.checked)}
							className="h-4 w-4 rounded border-secondary-300 text-primary-600 focus:ring-primary-500"
						/>
						<label
							htmlFor={enabledId}
							className="text-sm text-secondary-700 dark:text-secondary-200"
						>
							Enabled
						</label>
					</div>

					<div className="flex justify-end gap-3 pt-2">
						<button
							type="button"
							onClick={onClose}
							className="btn-outline"
							disabled={isLoading}
						>
							Cancel
						</button>
						<button type="submit" className="btn-primary" disabled={isLoading}>
							{isLoading
								? "Saving…"
								: schedule
									? "Save Changes"
									: "Create Schedule"}
						</button>
					</div>
				</form>
			</div>
		</div>
	);
};

const DeletePatchScheduleModal = ({
	schedule,
	onClose,
	onConfirm,
	isLoading,
}) => (
	<div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
		<div className="bg-white dark:bg-secondary-800 rounded-lg p-6 w-full max-w-md">
			<div className="flex items-center gap-3 mb-4">
				<AlertTriangle className="h-6 w-6 text-danger-500 flex-shrink-0" />
				<h3 className="text-lg font-semibold text-secondary-900 dark:text-white">
					Delete Patch Schedule
				</h3>
			</div>
			<p className="text-sm text-secondary-600 dark:text-secondary-200 mb-6">
				Delete the schedule{" "}
				<span className="font-semibold">{schedule.name}</span> (
				{describeSchedule(schedule)})? Hosts in the group{" "}
				<span className="font-semibold">{schedule.host_group_name}</span> will
				no longer be patched by this schedule.
			</p>
			<div className="flex justify-end gap-3">
				<button
					type="button"
					onClick={onClose}
					className="btn-outline"
					disabled={isLoading}
				>
					Cancel
				</button>
				<button
					type="button"
					onClick={onConfirm}
					className="btn-danger"
					disabled={isLoading}
				>
					{isLoading ? "Deleting…" : "Delete Schedule"}
				</button>
			</div>
		</div>
	</div>
);

export default PatchSchedules;
