# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.17"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.17/gestaltd-macos-arm64.tar.gz"
      sha256 "a3a4f09e0c22e4241eafa14cb42dcedf2232d9b8463e05eae80144830dfbb191"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.17/gestaltd-macos-x86_64.tar.gz"
      sha256 "3e6d7a42d70b2117f7801f769d0fe96159e5f16a564ca20851e23ef09dcfa07e"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.17/gestaltd-linux-arm64.tar.gz"
      sha256 "c5018bbdfb083ca9ecebec9a081c53621fadcc6afb2ef7c3c0b7ad518e9e5e2e"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.17/gestaltd-linux-x86_64.tar.gz"
      sha256 "134068e1bec2367a447a2402a552ef5b112e12eec0733c38a3478465754388a2"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
