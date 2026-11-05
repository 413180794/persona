import { t } from "../i18n";

/** 「推荐」 beside the option Claude suggests (its "(Recommended)" suffix, iOS RecommendedTag). */
export function RecommendedTag() {
  return <span className="ml-1.5 inline-block rounded-[4px] bg-accent-tint px-1 align-[1px] text-[11px] font-medium whitespace-nowrap text-accent-ink">{t("chat-interactions.recommended")}</span>;
}
