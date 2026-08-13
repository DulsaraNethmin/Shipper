import { DeliveryLink } from "@/components/delivery-link";

/**
 * The route a delivery link opens (SHIP-120).
 *
 *     https://<portal>/j/<job-id>#<token>
 *
 * SHIP-23 left this route unbuilt on purpose — "a scaffold route that accepted a token would be a
 * shape inviting somebody to render a job beside a token this application never checked" — and
 * defining it was SHIP-120's decision to make. `lib/link.ts` holds the whole of that decision: why
 * the job identifier is in the path when the token already names one, why the token is in the
 * fragment, and where it goes once the page has it.
 *
 * `/j/` rather than `/job/` because this link is forwarded through whatever messaging channel the
 * transport provider already uses, and a fragment-bearing URL with a UUID in it is long enough
 * already. It has no other meaning: the path is opaque to the driver, and everything that decides
 * anything is behind it.
 *
 * # This is a server component wrapping a client one, and that split is deliberate
 *
 * Everything on the page is the client's, because the credential is in the fragment and no server
 * receives one. What is left here is the path parameter, which Next hands to a server component
 * without a round trip and which would otherwise be unwrapped with `use()` for no benefit. Nothing
 * on this side reads the token, so nothing on this side can leak it into a rendered payload.
 *
 * The identifier is deliberately absent from the document title and from every piece of metadata.
 * A title is what a browser writes into history, what a tab strip shows across a room, and what a
 * screenshot captures — and none of those needs to carry which delivery this is.
 */
export default async function DeliveryLinkPage(props: PageProps<"/j/[jobId]">) {
  const { jobId } = await props.params;

  return <DeliveryLink jobId={jobId} />;
}
