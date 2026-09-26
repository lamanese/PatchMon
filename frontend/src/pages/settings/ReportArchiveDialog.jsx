import { useQuery } from "@tanstack/react-query";
import { Download, Loader2, X } from "lucide-react";
import { useState } from "react";
import { useToast } from "../../contexts/ToastContext";
import { formatDate, formatDateOnly, notificationsAPI } from "../../utils/api";
import {
	downloadBlob,
	errorFromBlobResponse,
	filenameFromDisposition,
} from "../../utils/downloadBlob";
import { channelIcon } from "./ReportModal";

const ERROR_LABELS = {
	definition_invalid: "Invalid report definition",
	scope_invalid: "Host group scope invalid",
	no_hosts: "No hosts in scope",
	too_many_hosts: "Too many hosts",
	render_failed: "Rendering failed",
	pdf_too_large: "PDF too large",
	smtp_connect: "SMTP connection failed",
	smtp_auth: "SMTP login failed",
	smtp_rejected: "Rejected by mail server",
	smtp_temporary: "Temporary mail server error",
	smtp_timeout: "SMTP timeout",
	destination_invalid: "Destination invalid",
	delivery_failed: "Delivery failed",
	abandoned: "Abandoned",
};

const errorLabel = (code) => ERROR_LABELS[code] || code;

const STATUS_STYLES = {
	completed:
		"bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200",
	partial: "bg-amber-100 text-amber-800 dark:bg-amber-900 dark:text-amber-200",
	failed: "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200",
	pending:
		"bg-secondary-100 text-secondary-700 dark:bg-secondary-700 dark:text-secondary-200",
};

const DELIVERY_STYLES = {
	sent: "text-green-700 dark:text-green-300",
	failed: "text-red-700 dark:text-red-300",
	pending: "text-secondary-500",
};

const TH =
	"px-3 py-2 text-left text-xs font-medium text-secondary-500 dark:text-white uppercase tracking-wider";
const TD =
	"px-3 py-2 text-sm text-secondary-900 dark:text-white align-top whitespace-normal";

const ReportArchiveDialog = ({ report, onClose }) => {
	const toast = useToast();
	const [downloadingId, setDownloadingId] = useState(null);
	const tz = report?.timezone || undefined;

	const {
		data: runs = [],
		isLoading,
		isError,
		error,
	} = useQuery({
		queryKey: ["notifications", "scheduled-reports", report.id, "archive"],
		queryFn: () =>
			notificationsAPI.listReportArchive(report.id).then((r) => {
				if (!Array.isArray(r.data)) throw new Error("Unexpected response");
				return r.data;
			}),
	});

	const download = async (run) => {
		setDownloadingId(run.id);
		try {
			const res = await notificationsAPI.downloadReportArchivePdf(run.id);
			downloadBlob(
				res.data,
				filenameFromDisposition(
					res.headers?.["content-disposition"],
					"report.pdf",
				),
			);
		} catch (err) {
			toast.error(await errorFromBlobResponse(err, "Download failed"));
		} finally {
			setDownloadingId(null);
		}
	};

	return (
		<div
			className="fixed inset-0 bg-black/50 flex items-center justify-center z-50"
			onClick={onClose}
		>
			<div
				role="dialog"
				aria-modal="true"
				className="bg-white dark:bg-secondary-800 rounded-lg shadow-xl max-w-5xl w-full mx-4 max-h-[90vh] flex flex-col"
				onClick={(e) => e.stopPropagation()}
			>
				<div className="px-6 py-4 border-b border-secondary-200 dark:border-secondary-600 flex items-center justify-between">
					<div>
						<h3 className="text-lg font-semibold text-secondary-900 dark:text-white">
							Archive: {report.name}
						</h3>
						{Number.isInteger(report.archive_keep) && (
							<p className="text-xs text-secondary-500">
								Keeps the newest {report.archive_keep} runs; older runs are
								deleted when a run finishes.
							</p>
						)}
					</div>
					<button
						type="button"
						onClick={onClose}
						className="text-secondary-400 hover:text-secondary-600 dark:hover:text-white"
						aria-label="Close"
					>
						<X className="h-5 w-5" />
					</button>
				</div>
				<div className="px-6 py-4 overflow-y-auto">
					{isLoading && <Loader2 className="h-5 w-5 animate-spin mx-auto" />}
					{isError && (
						<p className="text-sm text-red-600 dark:text-red-400">
							{error?.response?.data?.error ||
								error?.message ||
								"Failed to load the archive"}
						</p>
					)}
					{!isLoading && !isError && runs.length === 0 && (
						<p className="text-sm text-secondary-500">No runs yet.</p>
					)}
					{!isLoading && !isError && runs.length > 0 && (
						<div className="overflow-x-auto">
							<table className="min-w-full divide-y divide-secondary-200 dark:divide-secondary-600">
								<thead className="bg-secondary-50 dark:bg-secondary-700">
									<tr>
										<th className={TH}>Created</th>
										<th className={TH}>Trigger</th>
										<th className={TH}>Status</th>
										<th className={TH}>Period</th>
										<th className={TH}>Hosts</th>
										<th className={TH}>Deliveries</th>
										<th className={TH}>PDF</th>
									</tr>
								</thead>
								<tbody className="bg-white dark:bg-secondary-800 divide-y divide-secondary-200 dark:divide-secondary-600">
									{runs.map((run) => {
										const deliveries = Array.isArray(run.deliveries)
											? run.deliveries
											: [];
										const groups = Array.isArray(run.group_names)
											? run.group_names
											: [];
										return (
											<tr key={run.id}>
												<td className={`${TD} whitespace-nowrap`}>
													{formatDate(run.created_at, tz)}
												</td>
												<td className={TD}>
													{run.trigger === "manual" ? "Manual" : "Scheduled"}
												</td>
												<td className={TD}>
													<span
														className={`px-2 py-0.5 text-xs font-medium rounded-md ${STATUS_STYLES[run.status] || STATUS_STYLES.pending}`}
													>
														{run.status || "pending"}
													</span>
													{run.error_code && (
														<div
															className="mt-1 text-xs text-red-700 dark:text-red-300"
															title={run.error_message || ""}
														>
															{errorLabel(run.error_code)}
														</div>
													)}
												</td>
												<td className={`${TD} whitespace-nowrap`}>
													{run.period_from && run.period_to
														? `${formatDateOnly(run.period_from, tz)} – ${formatDateOnly(run.period_to, tz)}`
														: " -"}
												</td>
												<td className={TD} title={groups.join(", ")}>
													{run.host_count ?? " -"}
												</td>
												<td className={TD}>
													{run.delivery_enabled === false ? (
														<span
															className="text-xs text-secondary-500"
															title="Delivery was off for this run; the report was rendered and archived only."
														>
															Not delivered (delivery off)
														</span>
													) : deliveries.length === 0 ? (
														<span className="text-xs text-secondary-500">
															None
														</span>
													) : (
														<ul className="space-y-1">
															{deliveries.map((d) => (
																<li
																	key={d.id}
																	className="flex items-center gap-1.5 text-xs"
																>
																	{channelIcon(d.channel)}
																	<span className="break-all">
																		{d.recipient || d.destination_name || "?"}
																	</span>
																	<span
																		className={
																			DELIVERY_STYLES[d.status] ||
																			DELIVERY_STYLES.pending
																		}
																	>
																		{d.status}
																		{d.attempts > 1
																			? ` (${d.attempts} attempts)`
																			: ""}
																	</span>
																	{d.error_code && (
																		<span
																			className="text-red-700 dark:text-red-300 underline decoration-dotted cursor-help"
																			title={d.error_message || ""}
																		>
																			{errorLabel(d.error_code)}
																		</span>
																	)}
																</li>
															))}
														</ul>
													)}
												</td>
												<td className={TD}>
													<button
														type="button"
														className="text-primary-600 hover:text-primary-700 inline-flex items-center gap-1 text-xs disabled:opacity-40 disabled:cursor-not-allowed"
														disabled={!run.has_pdf || downloadingId === run.id}
														onClick={() => download(run)}
														title={
															run.has_pdf ? "Download PDF" : "No PDF stored"
														}
													>
														{downloadingId === run.id ? (
															<Loader2 className="h-3.5 w-3.5 animate-spin" />
														) : (
															<Download className="h-3.5 w-3.5" />
														)}
														Download
													</button>
												</td>
											</tr>
										);
									})}
								</tbody>
							</table>
						</div>
					)}
				</div>
				<div className="px-6 py-3 border-t border-secondary-200 dark:border-secondary-600 text-xs text-secondary-500">
					The 24 most recent runs are kept.
				</div>
			</div>
		</div>
	);
};

export default ReportArchiveDialog;
