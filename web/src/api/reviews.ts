import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import type { Doc } from "@/features/editor/schema";
import { api } from "./client";
import { contributorsQueryKey } from "./contributors";
import { pageQueryKey, pagesQueryKey, type PageInSpace } from "./pages";
import type { components } from "./schema";
import { spaceQueryKey } from "./spaces";
import { treeQueryKey } from "./tree";

type Wire = components["schemas"];

/** Who reviews what is published in a space, or in a branch of its pages. */
export type ReviewRule = Wire["ReviewRule"];
export type ReviewRuleInput = Wire["ReviewRuleInput"];
/** A draft put to its reviewers, as it was when submitted, without its body. */
export type ReviewSummary = Omit<Wire["PageReview"], "body">;
/** A review with its draft's body, which comes only with one review read alone. */
export type PageReview = ReviewSummary & { body?: Doc };
export type ReviewDecision = Wire["ReviewDecision"];
export type ReviewSubmitOptions = Wire["ReviewSubmitInput"];
/** How a version got past its review, as the history shows it. */
export type VersionReview = Wire["VersionReview"];
/** A pending review the caller may decide, with the page it is on. */
export interface WaitingReview {
  review: ReviewSummary;
  page: Wire["WaitingReview"]["page"];
  spaceKey: string;
}
/** Whether a page asks its readers to confirm, and whether the caller did. */
export type PageReadAsk = Wire["PageReadAsk"];
export type ReadConfirmations = Wire["ReadConfirmations"];

/** A side of a comparison that is the draft a review holds. */
export type ReviewSide = `review:${string}`;

export function reviewSide(reviewId: string): ReviewSide {
  return `review:${reviewId}`;
}

export function reviewRulesQueryKey(spaceKey: string) {
  return ["review-rules", spaceKey.toUpperCase()] as const;
}

export function reviewsQueryKey(pageId: string) {
  return [...pageQueryKey(pageId), "reviews"] as const;
}

export const waitingReviewsQueryKey = ["reviews", "waiting"] as const;

export function readConfirmationsQueryKey(pageId: string) {
  return [...pageQueryKey(pageId), "read-confirmations"] as const;
}

/** The space's rules: the space's own, with page null, and one per branch that has its own. */
export function useReviewRules(spaceKey: string, enabled = true) {
  return useQuery({
    queryKey: reviewRulesQueryKey(spaceKey),
    queryFn: async (): Promise<ReviewRule[]> => (await api.GET("/spaces/{spaceKey}/review-rules", { params: { path: { spaceKey } } })).data!.rules,
    enabled,
  });
}

// A rule decides what every page below it carries, so the space's rules and
// every page read so far are asked again.
function rulesChanged(queryClient: QueryClient) {
  return Promise.all([queryClient.invalidateQueries({ queryKey: ["review-rules"] }), queryClient.invalidateQueries({ queryKey: pagesQueryKey })]);
}

/** Sets the rule of a whole space, or with pageId the rule of that page and every page below it. */
export function useSetReviewRule(target: { spaceKey: string } | { pageId: string }) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (input: ReviewRuleInput): Promise<ReviewRule> =>
      "pageId" in target
        ? (await api.PUT("/pages/{pageID}/review-rule", { params: { path: { pageID: target.pageId } }, body: input })).data!.rule
        : (await api.PUT("/spaces/{spaceKey}/review-rule", { params: { path: { spaceKey: target.spaceKey } }, body: input })).data!.rule,
    onSuccess: () => rulesChanged(queryClient),
  });
}

/** Removes the rule of a whole space, or with pageId of a branch, so the space's holds there again. */
export function useRemoveReviewRule(target: { spaceKey: string } | { pageId: string }) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      if ("pageId" in target) await api.DELETE("/pages/{pageID}/review-rule", { params: { path: { pageID: target.pageId } } });
      else await api.DELETE("/spaces/{spaceKey}/review-rule", { params: { path: { spaceKey: target.spaceKey } } });
    },
    onSuccess: () => rulesChanged(queryClient),
  });
}

/** The page's reviews, the latest first, without their drafts' bodies. */
export function useReviews(pageId: string) {
  return useQuery({
    queryKey: reviewsQueryKey(pageId),
    queryFn: async (): Promise<PageReview[]> =>
      (await api.GET("/pages/{pageID}/reviews", { params: { path: { pageID: pageId } } })).data!.reviews as PageReview[],
  });
}

/** One review with its draft's body and every decision on it. */
export function useReview(pageId: string, reviewId: string) {
  return useQuery({
    queryKey: [...reviewsQueryKey(pageId), reviewId],
    queryFn: async (): Promise<PageReview> =>
      (await api.GET("/pages/{pageID}/reviews/{reviewID}", { params: { path: { pageID: pageId, reviewID: reviewId } } })).data!.review as PageReview,
  });
}

// The page carries its pending review; when one is published the page, its
// history, every comparison and who contributed change as with a publish.
function reviewChanged(queryClient: QueryClient, pageId: string, spaceKey: string) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: pageQueryKey(pageId) }),
    queryClient.invalidateQueries({ queryKey: waitingReviewsQueryKey }),
    queryClient.invalidateQueries({ queryKey: treeQueryKey }),
    queryClient.invalidateQueries({ queryKey: spaceQueryKey(spaceKey) }),
    queryClient.invalidateQueries({ queryKey: contributorsQueryKey }),
  ]);
}

/** Puts the caller's saved draft of the page to its reviewers instead of publishing it. */
export function useSubmitReview(pageId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (options: ReviewSubmitOptions): Promise<PageReview> =>
      (await api.POST("/pages/{pageID}/reviews", { params: { path: { pageID: pageId } }, body: options })).data!.review as PageReview,
    onSuccess: (review) => {
      queryClient.setQueryData<PageInSpace>(pageQueryKey(pageId), (current) =>
        current ? { ...current, page: { ...current.page, review: { ...current.page.review, pending: review } } } : current,
      );
      return Promise.all([
        queryClient.invalidateQueries({ queryKey: pageQueryKey(pageId) }),
        queryClient.invalidateQueries({ queryKey: waitingReviewsQueryKey }),
      ]);
    },
  });
}

/** The pending reviews the caller may decide, in every space, the oldest first. */
export function useWaitingReviews(enabled = true) {
  return useQuery({
    queryKey: waitingReviewsQueryKey,
    queryFn: async (): Promise<WaitingReview[]> =>
      // The server sends each review with its page beside its own fields; the screens read them apart.
      (await api.GET("/reviews/waiting")).data!.reviews.map(({ page, spaceKey, ...review }) => ({ review: review as ReviewSummary, page, spaceKey })),
    enabled,
  });
}

/** What a reviewer says: approve, or ask for changes with a comment. */
export type DecisionOptions = Wire["ReviewDecisionInput"];

/** A reviewer's word on a pending review; the approval that completes it publishes the draft. */
export function useDecideReview(pageId: string, spaceKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ reviewId, ...input }: DecisionOptions & { reviewId: string }) =>
      (await api.POST("/pages/{pageID}/reviews/{reviewID}/decisions", { params: { path: { pageID: pageId, reviewID: reviewId } }, body: input })).data!,
    onSuccess: () => reviewChanged(queryClient, pageId, spaceKey),
  });
}

/** The author takes a pending review back; the draft stays theirs. */
export function useWithdrawReview(pageId: string, spaceKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (reviewId: string) =>
      (await api.POST("/pages/{pageID}/reviews/{reviewID}/withdraw", { params: { path: { pageID: pageId, reviewID: reviewId } } })).data!.review,
    onSuccess: () => reviewChanged(queryClient, pageId, spaceKey),
  });
}

/** An administrator publishes a pending review without its approvals, for a reason. */
export function useSkipReview(pageId: string, spaceKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ reviewId, reason }: { reviewId: string; reason: string }) =>
      (await api.POST("/pages/{pageID}/reviews/{reviewID}/skip", { params: { path: { pageID: pageId, reviewID: reviewId } }, body: { reason } })).data!,
    onSuccess: () => reviewChanged(queryClient, pageId, spaceKey),
  });
}

function setReadAsk(queryClient: QueryClient, pageId: string, readAsk: PageReadAsk) {
  queryClient.setQueryData<PageInSpace>(pageQueryKey(pageId), (current) => (current ? { ...current, page: { ...current.page, readAsk } } : current));
  return queryClient.invalidateQueries({ queryKey: readConfirmationsQueryKey(pageId) });
}

/** Asks the page's readers to confirm from its current version on, again when it asked before; false stops asking. */
export function useSetReadAsk(pageId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (ask: boolean): Promise<PageReadAsk> =>
      (await api.PUT("/pages/{pageID}/read-ask", { params: { path: { pageID: pageId } }, body: { ask } })).data!.readAsk,
    onSuccess: (readAsk) => setReadAsk(queryClient, pageId, readAsk),
  });
}

/** The caller confirms they read the page as it is now. */
export function useConfirmRead(pageId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (): Promise<PageReadAsk> =>
      (await api.POST("/pages/{pageID}/read-confirmations", { params: { path: { pageID: pageId } } })).data!.readAsk,
    onSuccess: (readAsk) => setReadAsk(queryClient, pageId, readAsk),
  });
}

/** Who confirmed reading the page since it asked, and who of its readers has not; for its editors. */
export function useReadConfirmations(pageId: string, enabled = true) {
  return useQuery({
    queryKey: readConfirmationsQueryKey(pageId),
    queryFn: async (): Promise<ReadConfirmations> =>
      (await api.GET("/pages/{pageID}/read-confirmations", { params: { path: { pageID: pageId } } })).data!.readers,
    enabled,
  });
}
