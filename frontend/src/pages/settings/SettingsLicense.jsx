import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
	AlertCircle,
	AlertTriangle,
	BadgeCheck,
	CheckCircle,
	Info,
	Lock,
	Save,
} from "lucide-react";
import { useEffect, useState } from "react";
import { useAuth } from "../../contexts/AuthContext";
import { licenseAPI } from "../../utils/api";

const STATUS_META = {
	ok: {
		label: "Within licence",
		chip: "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200",
		bar: "bg-green-500",
	},
	over_limit: {
		label: "Over licence limit",
		chip: "bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200",
		bar: "bg-yellow-500",
	},
	over_tolerance: {
		label: "Over tolerance",
		chip: "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200",
		bar: "bg-red-500",
	},
	unlicensed: {
		label: "No licence configured",
		chip: "bg-secondary-100 text-secondary-700 dark:bg-secondary-700 dark:text-secondary-200",
		bar: "bg-secondary-400",
	},
};

const SettingsLicense = () => {
	const queryClient = useQueryClient();
	const { user } = useAuth();
	const isSuperAdmin = user?.role === "superadmin";

	const {
		data: licenseData,
		isLoading,
		error,
	} = useQuery({
		queryKey: ["license"],
		queryFn: () => licenseAPI.get().then((res) => res.data),
	});

	const [maxHosts, setMaxHosts] = useState("");
	const [packageName, setPackageName] = useState("");
	const [enforce, setEnforce] = useState(false);

	useEffect(() => {
		if (licenseData) {
			setMaxHosts(licenseData.max_hosts ?? "");
			setPackageName(licenseData.package ?? "");
			setEnforce(licenseData.enforce ?? false);
		}
	}, [licenseData]);

	const saveMutation = useMutation({
		mutationFn: (data) => licenseAPI.update(data).then((res) => res.data),
		onSuccess: () => {
			queryClient.invalidateQueries(["license"]);
			queryClient.invalidateQueries(["dashboardStats"]);
		},
	});

	if (isLoading) {
		return (
			<div className="flex items-center justify-center h-64">
				<div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary-600"></div>
			</div>
		);
	}

	if (error) {
		return (
			<div className="bg-red-50 dark:bg-red-900 border border-red-200 dark:border-red-700 rounded-md p-4">
				<div className="flex">
					<AlertCircle className="h-5 w-5 text-red-400 dark:text-red-300" />
					<div className="ml-3">
						<h3 className="text-sm font-medium text-red-800 dark:text-red-200">
							Error loading licence
						</h3>
						<p className="mt-1 text-sm text-red-700 dark:text-red-300">
							{error.message || "Failed to load licence"}
						</p>
					</div>
				</div>
			</div>
		);
	}

	const status = licenseData?.status || "unlicensed";
	const meta = STATUS_META[status] || STATUS_META.unlicensed;
	const locked = licenseData?.locked === true;
	const canEdit = isSuperAdmin && !locked;
	const licensed = licenseData?.max_hosts ?? null;
	const active = licenseData?.active_count ?? 0;
	const pending = licenseData?.pending_count ?? 0;
	const usedSlots = licenseData?.used_slots ?? active + pending;
	const hardLimit = licenseData?.hard_limit ?? null;
	const usagePct =
		licensed && licensed > 0
			? Math.min(100, Math.round((usedSlots / licensed) * 100))
			: 0;

	const handleSave = () => {
		const parsed = Number.parseInt(maxHosts, 10);
		saveMutation.mutate({
			max_hosts: Number.isNaN(parsed) ? null : parsed,
			enforce,
			package: packageName.trim() === "" ? null : packageName.trim(),
		});
	};

	return (
		<div className="space-y-6">
			{/* Header */}
			<div className="flex items-center mb-6">
				<BadgeCheck className="h-6 w-6 text-primary-600 mr-3" />
				<div>
					<h2 className="text-xl font-semibold text-secondary-900 dark:text-white">
						Host Licence
					</h2>
					<p className="text-sm text-secondary-600 dark:text-white mt-1">
						Licensed host count for this instance
						{licenseData?.package ? ` — ${licenseData.package}` : ""}
					</p>
				</div>
			</div>

			{/* Usage */}
			<div className="bg-white dark:bg-secondary-800 rounded-lg border border-secondary-200 dark:border-secondary-700 p-6">
				<div className="flex items-center justify-between mb-4">
					<h3 className="text-lg font-medium text-secondary-900 dark:text-white">
						Usage
					</h3>
					<span
						className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${meta.chip}`}
					>
						{meta.label}
					</span>
				</div>

				<p className="text-2xl font-semibold text-secondary-900 dark:text-white">
					{active}
					<span className="text-base font-normal text-secondary-500 dark:text-secondary-300">
						{" "}
						active / {pending} pending /{" "}
						{licensed !== null ? licensed : "unlimited"} licensed
					</span>
				</p>

				{licensed !== null && (
					<>
						<div className="mt-4 h-2 w-full rounded-full bg-secondary-200 dark:bg-secondary-700 overflow-hidden">
							<div
								className={`h-2 rounded-full ${meta.bar}`}
								style={{ width: `${usagePct}%` }}
							/>
						</div>
						<p className="mt-2 text-xs text-secondary-500 dark:text-secondary-300">
							{usedSlots} of {licensed} licensed slots used (active + pending,{" "}
							{licenseData?.tolerance_pct ?? 10}% tolerance up to {hardLimit}).
							{licenseData?.enforce
								? ` New host registrations are blocked once ${hardLimit} slots are used.`
								: " Enforcement is off: exceeding the limit only shows a warning."}
						</p>
					</>
				)}
			</div>

			{/* Locked / read-only notes */}
			{locked && (
				<div className="bg-secondary-50 dark:bg-secondary-800/50 border border-secondary-200 dark:border-secondary-700 rounded-lg p-4">
					<div className="flex items-center text-sm">
						<Lock className="h-4 w-4 text-secondary-500 mr-2 flex-shrink-0" />
						<span className="text-secondary-700 dark:text-white">
							The licence is managed by your provider via the server environment
							(PM_LICENSE_MAX_HOSTS) and cannot be changed here.
						</span>
					</div>
				</div>
			)}
			{!locked && !isSuperAdmin && (
				<div className="bg-secondary-50 dark:bg-secondary-800/50 border border-secondary-200 dark:border-secondary-700 rounded-lg p-4">
					<div className="flex items-center text-sm">
						<Info className="h-4 w-4 text-secondary-500 mr-2 flex-shrink-0" />
						<span className="text-secondary-700 dark:text-white">
							Only superadmins can change the licence settings.
						</span>
					</div>
				</div>
			)}

			{/* Edit form (superadmin, not env-managed) */}
			{canEdit && (
				<div className="bg-white dark:bg-secondary-800 rounded-lg border border-secondary-200 dark:border-secondary-700 p-6 space-y-4">
					<h3 className="text-lg font-medium text-secondary-900 dark:text-white">
						Licence Settings
					</h3>

					<div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
						<div>
							<label
								htmlFor="license-max-hosts"
								className="block text-sm font-medium text-secondary-700 dark:text-secondary-200 mb-1"
							>
								Licensed hosts
							</label>
							<input
								id="license-max-hosts"
								type="number"
								min="1"
								value={maxHosts}
								onChange={(e) => setMaxHosts(e.target.value)}
								placeholder="unlimited"
								className="block w-full rounded-md border border-secondary-300 dark:border-secondary-600 bg-white dark:bg-secondary-700 px-3 py-2 text-sm text-secondary-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-primary-500"
							/>
							<p className="mt-1 text-xs text-secondary-500 dark:text-secondary-300">
								Leave empty for unlimited (no licence).
							</p>
						</div>
						<div>
							<label
								htmlFor="license-package"
								className="block text-sm font-medium text-secondary-700 dark:text-secondary-200 mb-1"
							>
								Package name (optional)
							</label>
							<input
								id="license-package"
								type="text"
								maxLength={200}
								value={packageName}
								onChange={(e) => setPackageName(e.target.value)}
								placeholder='e.g. "Package 3, up to 200 VMs"'
								className="block w-full rounded-md border border-secondary-300 dark:border-secondary-600 bg-white dark:bg-secondary-700 px-3 py-2 text-sm text-secondary-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-primary-500"
							/>
						</div>
					</div>

					<div className="flex items-start justify-between pt-2">
						<div className="flex-1 pr-4">
							<p className="text-sm font-medium text-secondary-900 dark:text-white">
								Enforce limit
							</p>
							<p className="text-sm text-secondary-600 dark:text-secondary-300">
								Block new host registrations once the licensed count plus{" "}
								{licenseData?.tolerance_pct ?? 10}% tolerance is used. Existing
								hosts keep working either way.
							</p>
						</div>
						<button
							type="button"
							onClick={() => setEnforce(!enforce)}
							className={`relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-md border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2 ${
								enforce
									? "bg-primary-600"
									: "bg-secondary-200 dark:bg-secondary-700"
							}`}
						>
							<span
								className={`inline-block h-5 w-5 transform rounded-md bg-white shadow ring-0 transition duration-200 ease-in-out ${
									enforce ? "translate-x-5" : "translate-x-0"
								}`}
							/>
						</button>
					</div>

					<div className="pt-2 border-t border-secondary-200 dark:border-secondary-700">
						<button
							type="button"
							onClick={handleSave}
							disabled={saveMutation.isPending}
							className="inline-flex items-center px-4 py-2 border border-transparent text-sm font-medium rounded-md text-white bg-primary-600 hover:bg-primary-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-primary-500 disabled:opacity-50"
						>
							{saveMutation.isPending ? (
								<>
									<div className="animate-spin rounded-full h-4 w-4 border-b-2 border-white mr-2"></div>
									Saving...
								</>
							) : (
								<>
									<Save className="h-4 w-4 mr-2" />
									Save Licence
								</>
							)}
						</button>
					</div>

					{saveMutation.isSuccess && (
						<div className="bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-700 rounded-md p-3">
							<div className="flex">
								<CheckCircle className="h-4 w-4 text-green-400 dark:text-green-300 mt-0.5" />
								<p className="ml-2 text-sm text-green-700 dark:text-green-300">
									Licence settings updated
								</p>
							</div>
						</div>
					)}
					{saveMutation.isError && (
						<div className="bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-700 rounded-md p-3">
							<div className="flex">
								<AlertTriangle className="h-4 w-4 text-red-400 dark:text-red-300 mt-0.5" />
								<p className="ml-2 text-sm text-red-700 dark:text-red-300">
									{saveMutation.error?.response?.data?.error ||
										saveMutation.error?.message ||
										"Failed to update licence settings"}
								</p>
							</div>
						</div>
					)}
				</div>
			)}
		</div>
	);
};

export default SettingsLicense;
