import { useEffect, useRef } from "react";
import { useToast } from "@/components/ui";

/** Says once, in a toast, how something that ended on another site turned out, such as connecting an account. */
export function useArrivalToast(message: string | undefined) {
  const toast = useToast();
  // Kept across the second run of an effect a development build makes, so it is said once.
  const told = useRef(false);
  useEffect(() => {
    if (!message || told.current) return;
    told.current = true;
    toast.success(message);
  }, [message, toast]);
}
