import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
	AlertTriangle,
	CalendarClock,
	Edit,
	Plus,
	Power,
	Trash2,
} from "lucide-react";
import { useId, useState } from "react";
import { useToast } from "../contexts/ToastContext";
import { formatDate, hostGroupsAPI, rebootSchedulesAPI } from "../utils/api";

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
	return `Every ${WEEKDAYS[s.weekday] ?? "?"} ${s.time_of_day} (${s.timezone})`;
};

const RebootSchedules = () => {
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
		queryKey: ["rebootSchedules"],
		queryFn: () => rebootSchedulesAPI.list().then((res) => res.data),
		refetchInterval: 60000,
	});

	const { data: hostGroups } = useQuery({
		queryKey: ["hostGroups"],
		queryFn: () => hostGroupsAPI.list().then((res) => res.data),
	});

	const invalidate = () =>
		queryClient.invalidateQueries({ queryKey: ["rebootSchedules"] });

	const apiError = (err, fallback) =>
		err?.response?.data?.error || err?.message || fallback;

	const createMutation = useMutation({
		mutationFn: (data) => rebootSchedulesAPI.create(data),
		onSuccess: () => {
			invalidate();
			setShowFormModal(false);
			toast.success("Reboot schedule created");
		},
		onError: (err) => toast.error(apiError(err, "Failed to create schedule")),
	});

	const updateMutation = useMutation({
		mutationFn: ({ id, data }) => rebootSchedulesAPI.update(id, data),
		onSuccess: () => {
			invalidate();
			setShowFormModal(false);
			setEditingSchedule(null);
			toast.success("Reboot schedule updated");
		},
		onError: (err) => toast.error(apiError(err, "Failed to update schedule")),
	});

	const toggleMutation = useMutation({
		mutationFn: ({ id, enabled }) => rebootSchedulesAPI.update(id, { enabled }),
		onSuccess: invalidate,
		onError: (err) => toast.error(apiError(err, "Failed to toggle schedule")),
	});

	const deleteMutation = useMutation({
		mutationFn: (id) => rebootSchedulesAPI.delete(id),
		onSuccess: () => {
			invalidate();
			setScheduleToDelete(null);
			toast.success("Reboot schedule deleted");
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
					Scheduled reboots target one host group. At execution time only hosts
					with reboot allowed are rebooted; the PatchMon server's own host is
					always excluded. Missed one-time schedules expire instead of running
					late.
				</p>
				<button
					type="button"
					onClick={openCreate}
					className="btn-primary flex items-center gap-2 flex-shrink-0"
					title="Create reboot schedule"
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
										Only if required
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
											{s.only_if_required ? "Yes" : "No"}
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
										Error loading reboot schedules
									</h3>
									<p className="text-sm text-danger-700 dark:text-danger-300 mt-1">
										{error.message || "Failed to load reboot schedules"}
									</p>
								</div>
							</div>
						</div>
					</div>
				) : (
					<div className="p-12 text-center">
						<Power className="h-12 w-12 text-secondary-400 mx-auto mb-4" />
						<p className="text-secondary-500 dark:text-white">
							No reboot schedules found
						</p>
						<p className="text-sm text-secondary-400 dark:text-white mt-2">
							Click "Create Schedule" to schedule a reboot for a host group
						</p>
					</div>
				)}
			</div>

			{showFormModal && (
				<RebootScheduleFormModal
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
				<DeleteRebootScheduleModal
					schedule={scheduleToDelete}
					onClose={() => setScheduleToDelete(null)}
					onConfirm={() => deleteMutation.mutate(scheduleToDelete.id)}
					isLoading={deleteMutation.isPending}
				/>
			)}
		</div>
	);
};

const RebootScheduleFormModal = ({
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
	const onlyIfRequiredId = useId();
	const enabledId = useId();

	const [form, setForm] = useState(() => ({
		name: schedule?.name || "",
		host_group_id: schedule?.host_group_id || "",
		schedule_type: schedule?.schedule_type || "weekly",
		run_at_local: toLocalInput(schedule?.run_at),
		weekday: schedule?.weekday ?? 0,
		time_of_day: schedule?.time_of_day || "03:00",
		timezone: schedule?.timezone || browserTimezone(),
		only_if_required: schedule?.only_if_required ?? true,
		enabled: schedule?.enabled ?? true,
	}));

	const set = (field, value) => setForm((f) => ({ ...f, [field]: value }));

	const handleSubmit = (e) => {
		e.preventDefault();
		const data = {
			name: form.name,
			host_group_id: form.host_group_id,
			schedule_type: form.schedule_type,
			only_if_required: form.only_if_required,
			enabled: form.enabled,
		};
		if (form.schedule_type === "once") {
			data.run_at = toRFC3339(form.run_at_local);
			data.timezone = "UTC";
		} else {
			data.weekday = Number(form.weekday);
			data.time_of_day = form.time_of_day;
			data.timezone = form.timezone;
		}
		onSubmit(data);
	};

	return (
		<div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
			<div className="bg-white dark:bg-secondary-800 rounded-lg p-6 w-full max-w-md max-h-[90vh] overflow-y-auto">
				<h3 className="text-lg font-semibold text-secondary-900 dark:text-white mb-4">
					{schedule ? "Edit Reboot Schedule" : "Create Reboot Schedule"}
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
							placeholder="e.g., Sunday maintenance reboot"
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
							<div className="grid grid-cols-2 gap-3">
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
									The weekly time is evaluated in this timezone, so it stays at
									the same local time across DST changes.
								</p>
							</div>
						</>
					)}

					<div className="flex items-center gap-2">
						<input
							type="checkbox"
							id={onlyIfRequiredId}
							checked={form.only_if_required}
							onChange={(e) => set("only_if_required", e.target.checked)}
							className="h-4 w-4 rounded border-secondary-300 text-primary-600 focus:ring-primary-500"
						/>
						<label
							htmlFor={onlyIfRequiredId}
							className="text-sm text-secondary-700 dark:text-secondary-200"
						>
							Only reboot hosts that require a reboot
						</label>
					</div>

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

const DeleteRebootScheduleModal = ({
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
					Delete Reboot Schedule
				</h3>
			</div>
			<p className="text-sm text-secondary-600 dark:text-secondary-200 mb-6">
				Delete the schedule{" "}
				<span className="font-semibold">{schedule.name}</span> (
				{describeSchedule(schedule)})? Hosts in the group{" "}
				<span className="font-semibold">{schedule.host_group_name}</span> will
				no longer be rebooted by this schedule.
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

export default RebootSchedules;
