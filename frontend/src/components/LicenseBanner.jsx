import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, X } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import { useAuth } from "../contexts/AuthContext";
import { licenseAPI } from "../utils/api";

const DISMISS_KEY = "licenseBannerDismissed";

// Warning banner shown to settings managers when the instance is over its
// licensed host count: yellow over the limit, red once the 10% tolerance is
// consumed (then new host registrations are blocked when enforcement is on).
// Dismissible per browser session; an escalation (yellow -> red) re-shows it.
const LicenseBanner = () => {
	const { canManageSettings } = useAuth();
	const [dismissedFor, setDismissedFor] = useState(() => {
		try {
			return sessionStorage.getItem(DISMISS_KEY);
		} catch {
			return null;
		}
	});

	const { data: license } = useQuery({
		queryKey: ["license"],
		queryFn: () => licenseAPI.get().then((res) => res.data),
		enabled: canManageSettings(),
		refetchInterval: 5 * 60 * 1000,
	});

	const status = license?.status;
	const overTolerance = status === "over_tolerance";
	if (
		!license ||
		(status !== "over_limit" && !overTolerance) ||
		dismissedFor === status
	) {
		return null;
	}

	const dismiss = () => {
		setDismissedFor(status);
		try {
			sessionStorage.setItem(DISMISS_KEY, status);
		} catch {
			// session-only convenience; ignore storage errors
		}
	};

	const colors = overTolerance
		? "bg-red-100 text-red-800 border-red-300 dark:bg-red-900 dark:text-red-100 dark:border-red-700"
		: "bg-yellow-100 text-yellow-800 border-yellow-300 dark:bg-yellow-900 dark:text-yellow-100 dark:border-yellow-700";

	return (
		<div className={`border rounded-md px-4 py-3 mb-4 ${colors}`}>
			<div className="flex items-start">
				<AlertTriangle className="h-5 w-5 flex-shrink-0 mt-0.5" />
				<div className="ml-3 flex-1 text-sm">
					<span className="font-semibold">
						{overTolerance
							? "Licence exceeded beyond tolerance: "
							: "Licence limit exceeded: "}
					</span>
					{license.used_slots} host slots used ({license.active_count} active,{" "}
					{license.pending_count} pending) with {license.max_hosts} licensed
					{license.package ? ` (${license.package})` : ""}.{" "}
					{overTolerance && license.enforce
						? "New host registrations are blocked. "
						: ""}
					Please upgrade your licence.{" "}
					<Link to="/settings/license" className="underline font-medium">
						Licence settings
					</Link>
				</div>
				<button
					type="button"
					onClick={dismiss}
					className="ml-3 flex-shrink-0 opacity-70 hover:opacity-100"
					title="Dismiss for this session"
				>
					<X className="h-4 w-4" />
				</button>
			</div>
		</div>
	);
};

export default LicenseBanner;
