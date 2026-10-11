import { createRoute } from "@tanstack/react-router";
import { WaitingReviewsScreen } from "@/features/reviews/WaitingReviews";
import { appRoute } from "./app";

/** The reviews waiting on the caller across every space; a review's notification leads to its page, this lists them all. */
export const reviewsRoute = createRoute({ getParentRoute: () => appRoute, path: "/reviews", component: WaitingReviewsScreen });
