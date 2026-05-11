# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.23.multitenant.2"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.23.multitenant.2/gestaltd-macos-arm64.tar.gz"
      sha256 "9fc3680aa64d5871de62b2f8f54ade8b24cf19af5890adfc6a91e0042c7d6397"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.23.multitenant.2/gestaltd-macos-x86_64.tar.gz"
      sha256 "0618018df1846b6ade8db1d6f3e9d25dc5c7bb16b782e09defb5e1b775b266c6"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.23.multitenant.2/gestaltd-linux-arm64.tar.gz"
      sha256 "dc13710b023823b5ec9e5eb89f747034eb19cfe3a189e2102621a74b2426efed"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.23.multitenant.2/gestaltd-linux-x86_64.tar.gz"
      sha256 "96d07fa97da5e0cb4694738e146c3581056eaf5597e538acf14c2445404a8ad4"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
