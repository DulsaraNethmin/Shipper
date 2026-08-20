# Container images for the deployable binaries (SHIP-187).
#
# Included by the root Makefile's `-include mk/*.mk`, so this track adds its targets without
# editing a file three tracks may have open (Docs/10 §9.2). `make help` greps $(MAKEFILE_LIST),
# which covers included files, so everything documented here appears in the listing.
#
# CORE, VERSION, COMMIT and BUILT_AT come from the root Makefile — make variables are global, and
# this file is included after they are set. Passing them through as build arguments is the whole
# reason these targets exist rather than a docker command in a README: a container build has no
# git history to interrogate, so an image built by hand reports version "dev" from GET /health
# while the host build of the same commit reports the tag. One of those is a lie about what is
# running, and the tracker exists so nobody has to guess which.

# The five deployables. api, worker and notifier are what SHIP-187 names; migrate and topics are
# the one-shot deployment steps SHIP-188 cannot apply a schema or a topic set without. The
# Dockerfile's closing section carries the argument for including them.
IMAGE_TARGETS := api worker notifier migrate topics

# Overridable so a deployment can push to a registry without editing this file. Left as a bare
# name and a fixed tag by default because the local demonstration loads images from the daemon's
# own store and never pushes.
#
# The tag is `dev` rather than $(VERSION) deliberately. VERSION is `git describe`, which is a
# faithful description of the source and not always a legal image tag — a slash in a branch-shaped
# tag name is rejected by the registry, and discovering that during a release is worse than
# discovering it never. The version stamp is carried in the binary and in the OCI labels, where it
# has no character restrictions to trip over.
IMAGE_PREFIX ?= shipper
IMAGE_TAG    ?= dev

IMAGE_BUILD_ARGS := \
	--build-arg VERSION=$(VERSION) \
	--build-arg COMMIT=$(COMMIT) \
	--build-arg BUILT_AT=$(BUILT_AT)

.PHONY: images
images: $(addprefix image-,$(IMAGE_TARGETS)) ## Build container images for all five deployables (SHIP-187)
	@echo "built: $(foreach t,$(IMAGE_TARGETS),$(IMAGE_PREFIX)-$(t):$(IMAGE_TAG))"

# One pattern rule for all five. The context is $(CORE) and not the repository root — see the
# Dockerfile header: deploy/.env holds the local signing keys and the object-store credential, and
# a context rooted at the Go module cannot reach it by any instruction.
#
# **These targets are deliberately not .PHONY, and that is not an oversight.** make does not search
# implicit or pattern rules for a target it has been told is phony, so `.PHONY: image-api` next to
# an `image-%:` rule leaves `image-api` matching nothing — a target with no recipe, which make
# reports as successfully up to date. `make images` then printed five image names and built none,
# and the only thing that caught it was scripts/images-verify.sh asking the daemon whether the tags
# it had just been told about actually existed. Nothing named image-* is ever created on disk, so
# the pattern rule runs every time regardless and .PHONY was buying nothing to begin with.
image-%: ## Build one image — make image-api, image-worker, image-notifier, image-migrate, image-topics
	docker build \
		--file $(CORE)/Dockerfile \
		--target $* \
		$(IMAGE_BUILD_ARGS) \
		--tag $(IMAGE_PREFIX)-$*:$(IMAGE_TAG) \
		$(CORE)

.PHONY: images-verify
images-verify: ## Demonstrate SHIP-187: every image starts from environment configuration alone
	./scripts/images-verify.sh

.PHONY: images-clean
images-clean: ## Remove the locally built images
	-docker image rm $(foreach t,$(IMAGE_TARGETS),$(IMAGE_PREFIX)-$(t):$(IMAGE_TAG)) 2>/dev/null
	@echo "removed $(words $(IMAGE_TARGETS)) image tags, where present"
