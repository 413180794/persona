// 上传体检报告 (docs/health.md「体检报告」): the picked files go to the assistant's machine, into the health space's
// reports/, and the words that start the conversation name them.
import { MAX_FILE, quotePath, sendFile } from "../../lib/attachments";
import { t } from "../../i18n";

export const REPORT_FILES = "application/pdf,image/*";

/** Uploads the files one after another and returns the first message of their conversation.
 *  A file over the limit or a failed upload throws: nothing is started for part of what was picked. */
export async function uploadReports(mid: string, files: File[], dest: "health"): Promise<string> {
  const big = files.find((f) => f.size > MAX_FILE);
  if (big) throw new Error(t("chat-composer.file_too_large"));
  const paths: string[] = [];
  for (const f of files) paths.push(quotePath(await sendFile(mid, f, { dest })));
  return t("health.report_prompt", { paths: paths.join(" ") });
}
